#!/usr/bin/env python3
"""Tests for the spec patch layer. Offline: no network, no credentials.

    python3 scripts/test_fix_spec.py     (or: task spec-test)

The first two tests pin the bug that made `task spec-pull` corrupt the spec:
PyYAML implements YAML 1.1, so a bare `OFF` loads as boolean false, while the
Go and TypeScript generators read YAML 1.2 and see the string. The rest assert
the invariants the vendored spec has to keep for `task generate` to be
reproducible.
"""

import pathlib
import shutil
import subprocess
import sys
import tempfile
import unittest

import yaml

SCRIPTS = pathlib.Path(__file__).resolve().parent
REPO = SCRIPTS.parent
SPEC = REPO / "spec" / "openapi.yaml"

sys.path.insert(0, str(SCRIPTS))

import fix_spec  # noqa: E402  (path set above)
from fix_spec import SpecLoader, dump_spec  # noqa: E402

# Every scalar YAML 1.1 reads as a boolean, plus one control value.
YAML_11_BOOLEANS = ["OFF", "ON", "YES", "NO", "y", "n", "Off", "True", "FLASHBOOT"]


class RoundTrip(unittest.TestCase):
    def test_yaml_11_boolean_strings_survive_a_round_trip(self):
        spec = {
            "openapi": "3.1.0",
            "paths": {},
            "components": {
                "schemas": {
                    "FlashBoot": {"type": "string", "enum": list(YAML_11_BOOLEANS)},
                    "Env": {"examples": [{"JUPYTER_ENABLE_LAB": "yes"}]},
                }
            },
        }
        text = dump_spec(spec)
        # Both the YAML 1.2 loader used here and a stock YAML 1.1 reader must
        # give the strings back: the output has to be unambiguous for either.
        for reader in (SpecLoader, yaml.SafeLoader):
            reloaded = yaml.load(text, Loader=reader)
            self.assertEqual(
                reloaded["components"]["schemas"]["FlashBoot"]["enum"],
                YAML_11_BOOLEANS,
                f"{reader.__name__} did not read the enum back as strings",
            )
            self.assertEqual(
                reloaded["components"]["schemas"]["Env"]["examples"],
                [{"JUPYTER_ENABLE_LAB": "yes"}],
                f"{reader.__name__} did not read the example back as a string",
            )
        self.assertEqual(reloaded, spec)

    def test_stock_pyyaml_shows_why_the_loader_is_needed(self):
        # The bug this guards against, spelled out: plain `OFF` is false in 1.1.
        self.assertIs(yaml.safe_load("enum: [OFF]")["enum"][0], False)
        self.assertEqual(yaml.load("enum: [OFF]", Loader=SpecLoader)["enum"][0], "OFF")


class VendoredSpec(unittest.TestCase):
    def setUp(self):
        self.text = SPEC.read_text(encoding="utf-8")
        self.spec = yaml.load(self.text, Loader=SpecLoader)

    def test_patch_layer_run_is_a_no_op(self):
        """The tracked spec is patch-layer output, so a re-run must not touch it."""
        with tempfile.TemporaryDirectory() as tmp:
            copy = pathlib.Path(tmp) / "openapi.yaml"
            shutil.copyfile(SPEC, copy)
            result = subprocess.run(
                [sys.executable, str(SCRIPTS / "fix_spec.py"), str(copy)],
                capture_output=True,
                text=True,
                check=False,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn("already normalized", result.stdout)
            self.assertEqual(copy.read_text(encoding="utf-8"), self.text, "patch layer rewrote the spec")

    def test_flashboot_enum_is_strings_under_both_yaml_versions(self):
        for reader in (SpecLoader, yaml.SafeLoader):
            spec = yaml.load(self.text, Loader=reader)
            self.assertEqual(
                spec["components"]["schemas"]["FlashBoot"]["enum"],
                ["OFF", "FLASHBOOT", "PRIORITY_FLASHBOOT"],
                f"{reader.__name__} misread the FlashBoot enum",
            )

    def test_every_enum_names_its_generated_constants(self):
        """Without x-enum-varnames the generators emit `pods.ERROR`, `catalog.HIGH`."""
        for path, schema in enums(self.spec):
            self.assertIn("x-enum-varnames", schema, f"{path}: enum without constant names")
            self.assertEqual(
                len(schema["x-enum-varnames"]),
                len(schema["enum"]),
                f"{path}: constant names do not match the enum members",
            )

    def test_delegation_operations_declare_the_typed_auth_errors(self):
        for path, method in (
            ("/v2/registries/delegations", "get"),
            ("/v2/registries/delegations", "post"),
            ("/v2/registries/delegations/{id}", "delete"),
        ):
            responses = self.spec["paths"][path][method]["responses"]
            self.assertIn("401", responses, f"{method.upper()} {path}")
            self.assertIn("403", responses, f"{method.upper()} {path}")

    def test_no_concrete_aws_account_ids_in_examples(self):
        self.assertNotIn("418399314813", self.text)
        self.assertIn("123456789012", self.text)

    def test_the_spec_uses_the_current_brand_casing(self):
        """`RunPod` is the pre-rebrand styling; these strings become doc comments."""
        self.assertNotIn("RunPod", self.text)
        self.assertIn("Runpod official images", self.text)

    def test_every_operation_tag_is_declared_at_the_top_level(self):
        """Renderers take section order and descriptions from the tags array."""
        declared = {entry["name"] for entry in self.spec["tags"]}
        self.assertEqual(fix_spec.operation_tags(self.spec) - declared, set())

    def test_only_the_cursor_paged_operations_carry_a_pagination_block(self):
        """What the patched `Pagination` description promises the caller.

        It is the doc comment on every paged response struct, so a list
        operation that grows a pagination block without the query parameters —
        or the reverse — makes it a lie in generated code.
        """
        carries = {
            name
            for name, operation in operations(self.spec)
            if has_pagination(self.spec, success_schema(operation))
        }
        takes_cursor = {
            name for name, operation in operations(self.spec) if takes_parameter(self.spec, operation, "cursor")
        }
        self.assertEqual(carries, takes_cursor)
        self.assertEqual(
            carries,
            {"listPods", "listEndpoints", "listEndpointReleases", "listEndpointBuilds", "listTemplates"},
            "the set of paged operations moved — re-derive the README's paging paragraph too",
        )


class MovedAnchors(unittest.TestCase):
    """The module's central promise: an anchor that moved or vanished raises.

    The whole-document patches walk their entire registry, so these start from
    a copy of the vendored spec — patch-layer output, where every anchor
    resolves — and break exactly one node each, which is the drift a spec
    refresh produces. Nothing here touches the tracked file.
    """

    def setUp(self):
        self.spec = yaml.load(SPEC.read_text(encoding="utf-8"), Loader=SpecLoader)

    def test_a_vanished_named_schema_raises(self):
        del self.spec["components"]["schemas"]["PodStatus"]
        with self.assertRaises(fix_spec.PatchError):
            fix_spec.name_enum_constants(self.spec)

    def test_a_vanished_inline_enum_raises(self):
        del self.spec["components"]["schemas"]["QueueDelayScaling"]["properties"]["type"]["enum"]
        with self.assertRaises(fix_spec.PatchError):
            fix_spec.name_enum_constants(self.spec)

    def test_a_renamed_parameter_raises(self):
        parameters = self.spec["paths"]["/v2/catalog/templates"]["get"]["parameters"]
        parameters[0]["name"] = "origin"
        with self.assertRaises(fix_spec.PatchError):
            fix_spec.name_enum_constants(self.spec)

    def test_a_parameter_that_lost_its_enum_raises(self):
        # The case that raised TypeError instead: the anchor path carries an
        # int, so the error message could not be built.
        parameter = self.spec["paths"]["/v2/catalog/templates"]["get"]["parameters"][0]
        del parameter["schema"]["enum"]
        with self.assertRaises(fix_spec.PatchError):
            fix_spec.name_enum_constants(self.spec)

    def test_a_parameter_moved_behind_a_new_one_still_resolves(self):
        # What positional anchoring got wrong: upstream adding a parameter ahead
        # of `source` must not repoint the patch at the new node.
        parameters = self.spec["paths"]["/v2/catalog/templates"]["get"]["parameters"]
        parameters.insert(0, {"name": "limit", "in": "query", "schema": {"type": "integer"}})
        schema = fix_spec.resolve_parameter(self.spec, "/v2/catalog/templates", "get", "source")
        self.assertEqual(schema["enum"], ["official", "verified", "community"])

    def test_a_new_enum_member_without_a_constant_name_raises(self):
        self.spec["components"]["schemas"]["PodStatus"]["enum"].append("PAUSED")
        with self.assertRaises(fix_spec.PatchError):
            fix_spec.name_enum_constants(self.spec)

    def test_a_vanished_description_node_raises(self):
        del self.spec["components"]["schemas"]["Mounts"]["description"]
        with self.assertRaises(fix_spec.PatchError):
            fix_spec.normalize_public_descriptions(self.spec)

    def test_rewritten_description_text_raises(self):
        self.spec["components"]["schemas"]["EndpointType"]["description"] = "Rewritten upstream."
        with self.assertRaises(fix_spec.PatchError):
            fix_spec.normalize_public_descriptions(self.spec)

    def test_a_vanished_delegation_response_raises(self):
        responses = self.spec["paths"]["/v2/registries/delegations"]["get"]["responses"]
        for status in ("200", "401", "403"):
            responses.pop(status, None)
        with self.assertRaises(fix_spec.PatchError):
            fix_spec.add_delegation_auth_responses(self.spec)

    def test_a_changed_secret_example_raises(self):
        env = self.spec["components"]["schemas"]["BaseContainerConfig"]["properties"]["env"]
        env["examples"] = [{"JUPYTER_PASSWORD": "s3cr3t"}]
        with self.assertRaises(fix_spec.PatchError):
            fix_spec.use_neutral_secret_examples(self.spec)

    def test_a_tag_no_operation_carries_any_more_raises(self):
        self.spec["tags"] = [entry for entry in self.spec["tags"] if entry["name"] != "Clusters"]
        for path in [p for p in self.spec["paths"] if p.startswith("/v2/clusters")]:
            del self.spec["paths"][path]
        with self.assertRaises(fix_spec.PatchError):
            fix_spec.declare_operation_tags(self.spec)

    def test_the_declared_tag_to_anchor_after_vanishing_raises(self):
        self.spec["tags"] = [entry for entry in self.spec["tags"] if entry["name"] not in ("Pods", "Clusters")]
        with self.assertRaises(fix_spec.PatchError):
            fix_spec.declare_operation_tags(self.spec)


class Helpers(unittest.TestCase):
    def test_resolve_reports_the_full_path_including_list_indices(self):
        spec = {"components": {"schemas": {"CreatePodRequest": {"allOf": [{}]}}}}
        path = ("components", "schemas", "CreatePodRequest", "allOf", 1, "description")
        with self.assertRaises(fix_spec.PatchError) as raised:
            fix_spec.resolve(spec, path)
        self.assertIn("allOf/1/description", str(raised.exception))

    def test_account_ids_are_replaced_in_mapping_keys_too(self):
        # walk_strings visits keys because the spec carries data-bearing ones.
        spec = {"examples": [{"418399314813": "account"}]}
        fix_spec.use_placeholder_aws_account_ids(spec)
        self.assertEqual(spec["examples"], [{"123456789012": "account"}])

    def test_the_brand_sweep_leaves_identifiers_alone(self):
        # Prose is rebranded; a wire value or an identifier carries whatever
        # casing it was published with, and renaming one is a breaking change.
        spec = {"d": ["RunPod official images", "Terraform/RunPodProvider", "RunPod-Key-Go"]}
        self.assertEqual(fix_spec.use_current_brand_casing(spec), 1)
        self.assertEqual(spec["d"], ["Runpod official images", "Terraform/RunPodProvider", "RunPod-Key-Go"])


def operations(spec):
    """Every operation in the document, by operationId."""
    for item in spec["paths"].values():
        for operation in item.values():
            if isinstance(operation, dict) and "operationId" in operation:
                yield operation["operationId"], operation


def deref(spec, node):
    """A node with its local `$ref` chain followed; anything else unchanged."""
    while isinstance(node, dict) and "$ref" in node:
        node = fix_spec.resolve(spec, tuple(node["$ref"].removeprefix("#/").split("/")))
    return node


def success_schema(operation):
    """The 200 `application/json` schema, or None for an operation without one."""
    responses = operation.get("responses") or {}
    content = (responses.get("200") or {}).get("content") or {}
    return (content.get("application/json") or {}).get("schema")


def has_pagination(spec, schema):
    """Whether a response schema carries the pagination block, `allOf` included."""
    schema = deref(spec, schema)
    if not isinstance(schema, dict):
        return False
    if "pagination" in (schema.get("properties") or {}):
        return True
    return any(has_pagination(spec, member) for member in schema.get("allOf") or [])


def takes_parameter(spec, operation, name):
    return any(deref(spec, parameter).get("name") == name for parameter in operation.get("parameters") or [])


def enums(node, path=""):
    """Every schema in the document that declares an enum, inline ones included."""
    if isinstance(node, dict):
        if isinstance(node.get("enum"), list):
            yield path, node
        for key, value in node.items():
            yield from enums(value, f"{path}.{key}")
    elif isinstance(node, list):
        for index, value in enumerate(node):
            yield from enums(value, f"{path}[{index}]")


if __name__ == "__main__":
    unittest.main(verbosity=2)
