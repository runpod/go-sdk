#!/usr/bin/env python3
"""Patch layer for the vendored Runpod v2 OpenAPI spec.

This is the one place spec fixes live for this repo's vendored spec and
everything generated from it: a drift between the served spec and live API
behavior, or a value that must not ship in a public artifact, is registered
here — never hand-edited into spec/openapi.yaml or the generated code. Running
the layer also normalizes the file's formatting, so `spec/openapi.yaml` is
always this script's output and `task generate` is reproducible.

Usage:
    python3 scripts/fix_spec.py spec/openapi.yaml

Each patch is a named function taking the spec dict and mutating it in place;
it returns the number of edits it made, so a re-run over an already-patched
spec reports 0 instead of failing. A patch whose anchor has disappeared — the
node is gone, or upstream rewrote the text it targets — raises PatchError
instead of silently doing nothing. Keep every entry small, dated, and pointing
at the evidence for why it exists (issue link, observed live response).

YAML versions matter here. Stock PyYAML implements YAML 1.1, where the bare
scalars OFF, ON, YES and NO are booleans: a `yaml.safe_load` round-trip turns
the FlashBoot enum value `OFF` into `false` and the template example
`JUPYTER_ENABLE_LAB: yes` into `true` (11 sites in the 2026-08 spec). The
generators read YAML 1.2 — `gopkg.in/yaml.v3` here, `js-yaml` in the
TypeScript SDK — where all four are strings, so the corruption is invisible
from Python and lands in generated code. This module therefore loads with
SpecLoader (YAML 1.2 booleans), dumps with SpecDumper (which quotes every
string a YAML 1.1 reader would resolve to something else), and verifies the
round-trip under both readers before writing.
"""

import argparse
import copy
import re
import sys

import yaml

BOOL_TAG = "tag:yaml.org,2002:bool"
STR_TAG = "tag:yaml.org,2002:str"
SEQ_TAG = "tag:yaml.org,2002:seq"

# Booleans of the YAML 1.2 core schema, and the wider YAML 1.1 set that a 1.1
# reader would also resolve — every string matching the latter gets quoted.
YAML_12_BOOL = re.compile(r"^(?:true|True|TRUE|false|False|FALSE)$")
YAML_11_BOOL = re.compile(
    r"^(?:y|Y|n|N|yes|Yes|YES|no|No|NO|on|On|ON|off|Off|OFF"
    r"|true|True|TRUE|false|False|FALSE)$"
)

# Characters YAML treats as line breaks besides \n and \r. PyYAML emits them raw
# in a plain or block scalar, where a reader folds them into a space; the
# double-quoted style is the only one that escapes them (\N, \L, \P).
YAML_EXTRA_BREAKS = re.compile("[\x85\u2028\u2029]")

# A sequence stays on one line while its rendered flow form fits this width.
FLOW_WIDTH = 60

# Line width for folded and plain scalars.
TEXT_WIDTH = 100


class PatchError(Exception):
    """A patch could not apply — usually because upstream changed underneath it."""


class SpecLoader(yaml.SafeLoader):
    """SafeLoader resolving booleans the way YAML 1.2 and the generators do."""


SpecLoader.yaml_implicit_resolvers = {
    char: [(tag, pattern) for tag, pattern in resolvers if tag != BOOL_TAG]
    for char, resolvers in yaml.SafeLoader.yaml_implicit_resolvers.items()
}
SpecLoader.add_implicit_resolver(BOOL_TAG, YAML_12_BOOL, list("tTfF"))


class SpecDumper(yaml.SafeDumper):
    """SafeDumper that keeps block sequences indented under their key.

    Anchors and aliases are suppressed: a repeated node is written out in full
    rather than as `*id001`, because the spec is read by OpenAPI tooling that
    has no reason to handle YAML references.
    """

    def increase_indent(self, flow=False, indentless=False):
        return super().increase_indent(flow, False)

    def ignore_aliases(self, data):
        return True


def represent_str(dumper, value):
    """Multi-line text stays readable; ambiguous scalars get quoted."""
    if YAML_EXTRA_BREAKS.search(value):
        # The global volume name patterns carry NEL (U+0085) in a character class.
        return dumper.represent_scalar(STR_TAG, value, style='"')
    if "\n" in value.rstrip("\n"):
        # Author-chosen line breaks (bullet lists, PATCH-semantics blocks).
        return dumper.represent_scalar(STR_TAG, value, style="|")
    if value.endswith("\n"):
        # One logical line: fold it so long prose wraps instead of running off.
        return dumper.represent_scalar(STR_TAG, value, style=">")
    if YAML_11_BOOL.match(value):
        return dumper.represent_scalar(STR_TAG, value, style="'")
    return dumper.represent_scalar(STR_TAG, value)


def flow_width(value) -> int | None:
    """Rendered width of value in flow style, or None if it needs block style.

    Mappings always take block style: `security: [{bearerAuth: []}]` is legal
    but reads worse than the two-line form, and it is the shape reviewers scan.
    """
    if isinstance(value, str):
        return None if "\n" in value else len(value) + 2
    if value is None:
        return len("null")
    if isinstance(value, bool):
        return len("true") if value else len("false")
    if isinstance(value, (int, float)):
        return len(repr(value))
    if isinstance(value, list):
        widths = [flow_width(item) for item in value]
        if any(width is None for width in widths):
            return None
        return sum(widths) + 2 * len(widths) + 2
    return None


def represent_list(dumper, data):
    width = flow_width(data)
    inline = width is not None and width <= FLOW_WIDTH
    return dumper.represent_sequence(SEQ_TAG, data, flow_style=inline)


SpecDumper.add_representer(str, represent_str)
SpecDumper.add_representer(list, represent_list)


def dump_spec(spec: dict) -> str:
    return yaml.dump(
        spec,
        Dumper=SpecDumper,
        sort_keys=False,
        allow_unicode=True,
        default_flow_style=False,
        width=TEXT_WIDTH,
    )


def walk_strings(node, replace):
    """Apply replace() to every string in the document, in place.

    Mapping keys are rewritten too: the spec carries data-bearing keys (the
    `env` examples), so a value that must not ship can appear as one. A dict is
    rebuilt rather than mutated during iteration, which keeps key order.
    """
    if isinstance(node, dict):
        rewritten = {}
        for key, value in node.items():
            if isinstance(value, str):
                value = replace(value)
            else:
                walk_strings(value, replace)
            rewritten[replace(key) if isinstance(key, str) else key] = value
        node.clear()
        node.update(rewritten)
    elif isinstance(node, list):
        for index, value in enumerate(node):
            if isinstance(value, str):
                node[index] = replace(value)
            else:
                walk_strings(value, replace)


def label(path: tuple[str | int, ...]) -> str:
    """A path rendered for an error message; list indices are part of a path."""
    return "/".join(map(str, path))


def resolve(spec: dict, path: tuple[str | int, ...]):
    node = spec
    for key in path:
        if isinstance(node, list) and isinstance(key, int) and 0 <= key < len(node):
            node = node[key]
            continue
        if not isinstance(node, dict) or key not in node:
            raise PatchError(f"{label(path)}: no such node — retire or repoint this patch")
        node = node[key]
    return node


def resolve_parameter(spec: dict, path: str, method: str, name: str):
    """The schema of one named operation parameter.

    Parameters are a list, so anchoring on a position breaks silently the day
    upstream adds one ahead of it: the index still resolves, to a different
    node. This matches on the name instead.
    """
    for parameter in resolve(spec, ("paths", path, method, "parameters")):
        if not isinstance(parameter, dict) or parameter.get("name") != name:
            continue
        if "schema" not in parameter:
            raise PatchError(f"{method.upper()} {path}: the {name!r} parameter has no schema — retire or repoint this entry")
        return parameter["schema"]
    raise PatchError(f"{method.upper()} {path}: no {name!r} parameter — retire or repoint this entry")


# Every AWS account id in an example is the documented all-digits placeholder.
# Upstream ships one real-shaped 12-digit id and two 9-digit values that are
# not valid account ids at all.
ACCOUNT_ID_PLACEHOLDER = "123456789012"
ACCOUNT_ID_PATTERN = re.compile(r"\b(?:418399314813|123456789)(?!\d)")


def use_placeholder_aws_account_ids(spec: dict) -> int:
    """2026-08-28: keep concrete AWS account ids out of the published spec.

    `arn:aws:ecr:us-east-2:418399314813:...` is either a live Runpod account
    number or a fabricated one; both are wrong in a client-facing artifact, and
    its sibling examples used 9-digit values where AWS ids are 12 digits.
    """
    replaced = 0

    def replace(value: str) -> str:
        nonlocal replaced
        new_value, count = ACCOUNT_ID_PATTERN.subn(ACCOUNT_ID_PLACEHOLDER, value)
        replaced += count
        return new_value

    walk_strings(spec, replace)
    return replaced


# The company is Runpod. `RunPod` was the styling before the June 2025 rebrand
# and still lingers in upstream description prose. Identifiers keep whatever
# casing they were published with, so the pattern refuses a match that runs on
# into one (`RunPodProvider`, `RunPod-Key-Go`) or follows one; anything upstream
# adds in a form this skips is caught by the spec test, which asserts the
# vendored document carries no `RunPod` at all.
BRAND_PATTERN = re.compile(r"(?<![A-Za-z0-9_-])RunPod(?![A-Z0-9_-])")


def use_current_brand_casing(spec: dict) -> int:
    """2026-09-18: the served spec titles itself `Runpod REST API`, its prose does not.

    Five description strings still read `RunPod`, and each one reaches the
    generated Go doc comments in `lib/`, which is where a reader of the public
    SDK meets the brand.
    """
    replaced = 0

    def replace(value: str) -> str:
        nonlocal replaced
        new_value, count = BRAND_PATTERN.subn("Runpod", value)
        replaced += count
        return new_value

    walk_strings(spec, replace)
    return replaced


# Constant names for the enums that would otherwise generate bare screaming
# identifiers in a public package (`pods.ERROR`, `catalog.HIGH`,
# `serverless.OFF`, `templates.CPU`, `pods.Start`). Upstream already ships
# x-enum-varnames for six enums; these follow the same convention — schema name
# plus the value in CamelCase, acronyms kept uppercase. Keyed by value, so a new
# upstream member fails loudly here instead of generating a bare constant.
ENUM_VARNAMES: dict[str, dict[str, str]] = {
    "PodStatus": {
        "PROVISIONING": "PodStatusProvisioning",
        "STARTING": "PodStatusStarting",
        "RUNNING": "PodStatusRunning",
        "EXITED": "PodStatusExited",
        "ERROR": "PodStatusError",
        "TERMINATED": "PodStatusTerminated",
    },
    "ClusterType": {
        "APPLICATION": "ClusterTypeApplication",
        "TRAINING": "ClusterTypeTraining",
        "SLURM": "ClusterTypeSlurm",
        "RAY": "ClusterTypeRay",
    },
    "PodClusterRole": {
        "SLURM_CONTROLLER": "PodClusterRoleSlurmController",
        "SLURM_COMPUTE": "PodClusterRoleSlurmCompute",
        "RAY_HEAD": "PodClusterRoleRayHead",
        "RAY_WORKER": "PodClusterRoleRayWorker",
    },
    "PodAction": {
        "start": "PodActionStart",
        "stop": "PodActionStop",
        "restart": "PodActionRestart",
        "terminate": "PodActionTerminate",
    },
    "Cloud": {
        "SECURE": "CloudSecure",
        "COMMUNITY": "CloudCommunity",
    },
    "TemplateCategory": {
        "CPU": "TemplateCategoryCPU",
        "NVIDIA": "TemplateCategoryNVIDIA",
        "AMD": "TemplateCategoryAMD",
    },
    "FlashBoot": {
        "OFF": "FlashBootOff",
        "FLASHBOOT": "FlashBootEnabled",
        "PRIORITY_FLASHBOOT": "FlashBootPriority",
    },
    "AvailabilityLevel": {
        "NONE": "AvailabilityLevelNone",
        "LOW": "AvailabilityLevelLow",
        "MEDIUM": "AvailabilityLevelMedium",
        "HIGH": "AvailabilityLevelHigh",
    },
    "Product": {
        "POD": "ProductPod",
        "CLUSTER": "ProductCluster",
        "SERVERLESS": "ProductServerless",
    },
    "CpuProduct": {
        "POD": "CpuProductPod",
        "SERVERLESS": "CpuProductServerless",
    },
    "CatalogInclude": {
        "AVAILABILITY": "CatalogIncludeAvailability",
    },
    "DataCenterInclude": {
        "GPU_AVAILABILITY": "DataCenterIncludeGpuAvailability",
        "CPU_AVAILABILITY": "DataCenterIncludeCpuAvailability",
    },
    "GpuCloudFilter": {
        "SECURE": "GpuCloudFilterSecure",
        "COMMUNITY": "GpuCloudFilterCommunity",
    },
    "VolumeType": {
        "STANDARD": "VolumeTypeStandard",
        "HIGH_PERFORMANCE": "VolumeTypeHighPerformance",
    },
    "DataCenterRegion": {
        "NORTH_AMERICA": "DataCenterRegionNorthAmerica",
        "SOUTH_AMERICA": "DataCenterRegionSouthAmerica",
        "EUROPE": "DataCenterRegionEurope",
        "ASIA": "DataCenterRegionAsia",
        "MIDDLE_EAST": "DataCenterRegionMiddleEast",
        "AFRICA": "DataCenterRegionAfrica",
        "OCEANIA": "DataCenterRegionOceania",
        "ANTARCTICA": "DataCenterRegionAntarctica",
        "UNKNOWN": "DataCenterRegionUnknown",
    },
    "Compliance": {
        "GDPR": "ComplianceGDPR",
        "ISO_IEC_27001": "ComplianceISOIEC27001",
        "ISO_14001": "ComplianceISO14001",
        "PCI_DSS": "CompliancePCIDSS",
        "HITRUST": "ComplianceHITRUST",
        "SOC_1_TYPE_2": "ComplianceSOC1Type2",
        "SOC_2_TYPE_2": "ComplianceSOC2Type2",
        "SOC_3_TYPE_2": "ComplianceSOC3Type2",
        "ITAR": "ComplianceITAR",
        "FISMA_HIGH": "ComplianceFISMAHigh",
        "HIPAA": "ComplianceHIPAA",
        "RENEWABLE": "ComplianceRenewable",
    },
}

# The scaler discriminators are inline enums on a property, not named schemas;
# oapi-codegen names their type <Schema><Property>.
INLINE_ENUM_VARNAMES: dict[tuple[str, ...], dict[str, str]] = {
    ("QueueDelayScaling", "properties", "type"): {
        "QUEUE_DELAY": "QueueDelayScalingTypeQueueDelay",
    },
    ("RequestCountScaling", "properties", "type"): {
        "REQUEST_COUNT": "RequestCountScalingTypeRequestCount",
    },
}

# Enums that live on an operation parameter rather than under
# components/schemas, keyed by (path, method, parameter name).
PARAMETER_ENUM_VARNAMES: dict[tuple[str, str, str], dict[str, str]] = {
    ("/v2/catalog/templates", "get", "source"): {
        "official": "CatalogTemplateSourceOfficial",
        "verified": "CatalogTemplateSourceVerified",
        "community": "CatalogTemplateSourceCommunity",
    },
}


def enum_targets(spec: dict):
    """Every registered enum node, with its constant names and a message label."""
    for name, varnames in ENUM_VARNAMES.items():
        path = ("components", "schemas", name)
        yield label(path), resolve(spec, path), varnames
    for path, varnames in INLINE_ENUM_VARNAMES.items():
        full = ("components", "schemas") + path
        yield label(full), resolve(spec, full), varnames
    for (path, method, name), varnames in PARAMETER_ENUM_VARNAMES.items():
        yield (
            f"{method.upper()} {path} ?{name}",
            resolve_parameter(spec, path, method, name),
            varnames,
        )


def name_enum_constants(spec: dict) -> int:
    """2026-08-28: give every enum explicit generated constant names.

    Without x-enum-varnames the generators emit the raw value as the identifier,
    so a consumer imports `pods.ERROR`, `catalog.HIGH` or `serverless.OFF` from
    a public SDK. Upstream applies the extension to 6 of 20 enums; this covers
    the rest, including the two inline scaler discriminators.
    2026-09-17: the spec grew clusters (two named enums) and a catalog-templates
    `source` query parameter with an inline enum.
    """
    edits = 0
    for anchor, schema, varnames in enum_targets(spec):
        values = schema.get("enum")
        if not isinstance(values, list):
            raise PatchError(f"{anchor}: no enum to name — retire or repoint this entry")
        unnamed = [value for value in values if value not in varnames]
        if unnamed:
            raise PatchError(f"{anchor}: no constant name registered for {unnamed}")
        named = [varnames[member] for member in values]
        if schema.get("x-enum-varnames") == named:
            continue
        if "x-enum-varnames" in schema:
            raise PatchError(f"{anchor}: upstream now names these constants — retire this entry")
        rebuilt: dict = {}
        for key, value in schema.items():
            if key == "enum":
                rebuilt["x-enum-varnames"] = named
            rebuilt[key] = value
        schema.clear()
        schema.update(rebuilt)
        edits += 1
    return edits


# Public-description edits: (node path, exact upstream text, replacement). The
# upstream text is spelled out so a spec refresh that rewrites the sentence
# fails here instead of quietly dropping the fix.
DESCRIPTION_EDITS: list[tuple[tuple[str | int, ...], str, str]] = [
    # Server-internal structure ("the handler", "upstream") described to
    # external API consumers; the contract half is what callers need.
    (
        ("components", "schemas", "BaseMounts", "description"),
        "At-most-one\nof `persistent` or `network` may be set today (mutually exclusive,\n"
        "enforced at the handler with 400 if both are present). The `network`\n"
        "field is an array for forward compatibility with eventual\n"
        "multi-network-volume support, but `maxItems` is 1 today.",
        "At most one\nof `persistent` or `network` may be set: sending both is rejected with 400. The\n"
        "`network` field is an array for forward compatibility with\n"
        "multi-network-volume support; `maxItems` is 1.",
    ),
    (
        ("components", "schemas", "Mounts", "description"),
        "`persistent` is mutually exclusive\nwith both `network` and `global` (enforced at the handler with 400 if\n"
        "combined). The `network` field is an array for forward compatibility\n"
        "with eventual multi-network-volume support, but `maxItems` is 1 today.",
        "`persistent` is mutually exclusive\nwith both `network` and `global`: combining them is rejected with 400.\n"
        "The `network` field is an array for forward compatibility with\n"
        "multi-network-volume support; `maxItems` is 1.",
    ),
    (
        ("components", "schemas", "Mounts", "allOf", 1, "properties", "global", "description"),
        "`persistent` (rejected with 400): upstream drops a persistent\ndisk whenever a volume is attached.",
        "`persistent` (rejected with 400): a persistent disk is dropped\nwhenever a volume is attached.",
    ),
    (
        ("components", "schemas", "TemplateMounts", "description"),
        "Templates support only a\nsingle persistent mount today; any `network` property is rejected\n"
        "with 422 by the schema validator.",
        "Templates support a single\npersistent mount; any `network` property is rejected with 422.",
    ),
    (
        ("components", "schemas", "CreatePodRequest", "allOf", 1, "description"),
        "Exactly one of `gpu` or `cpu`\nmust be set — enforced at the handler layer. For CPU pods, memory\n"
        "is derived by the API from the selected flavor's RAM multiplier;\n"
        "clients provide only CPU flavor and vCPU count. CPU pods support\n"
        "container disk and network volumes only; `mounts.persistent` is\n"
        "invalid when `cpu` is set.",
        "Exactly one of `gpu` or `cpu`\nmust be set. For CPU pods, memory is derived by the API from the\n"
        "selected flavor's RAM multiplier; clients provide only CPU flavor\n"
        "and vCPU count. CPU pods support container disk and network\n"
        "volumes only; `mounts.persistent` is invalid when `cpu` is set.",
    ),
    (
        (
            "components", "schemas", "CreatePodRequest", "allOf", 1,
            "properties", "globalNetworking", "description",
        ),
        "global-networking-enabled data center (both enforced upstream).",
        "global-networking-enabled data center.",
    ),
    (
        (
            "components", "schemas", "UpdatePodRequest", "allOf", 1,
            "properties", "globalNetworking", "description",
        ),
        "global-networking-enabled data center (both enforced upstream).",
        "global-networking-enabled data center.",
    ),
    # One fact, stated twice in two paraphrases, both dated by "today".
    (
        ("components", "parameters", "CatalogIncludeParam", "description"),
        "Comma-separated optional expansions. Supported value today: AVAILABILITY."
        " This may expand with more include values in the future.",
        "Comma-separated optional expansions; see `CatalogInclude` for the supported values.",
    ),
    (
        ("components", "schemas", "CatalogInclude", "description"),
        "Catalog include expansion. Only AVAILABILITY is supported today;"
        " additional include values may be added in the future.",
        "Catalog include expansion. `AVAILABILITY` is the only supported value.",
    ),
    # "modern" ages into a wrong statement without anyone editing it.
    (
        ("components", "schemas", "EndpointType", "description"),
        "Request-routing semantics for a modern serverless endpoint.",
        "Request-routing semantics for a serverless endpoint.",
    ),
    # 2026-09-23: pagination prose the schemas contradict. Five of the twenty-two
    # list operations take `cursor`/`limit` and carry the block (pods, endpoints,
    # endpoint releases, endpoint builds, templates); the other seventeen return a
    # bare list schema with no `pagination` property, so a caller who follows
    # either sentence writes code that does not compile.
    (
        ("components", "schemas", "Pagination", "description"),
        "Cursor-pagination metadata, uniform across list endpoints. Every response carries it:"
        " follow `nextCursor` while `hasNextPage` is true to walk the full result set.",
        "Cursor-pagination metadata. A list response carries it only where the operation takes the"
        " `cursor` and `limit` query parameters; every other list endpoint returns its bare list."
        " Where it is present, follow `nextCursor` while `hasNextPage` is true to walk the full"
        " result set.",
    ),
    (
        ("paths", "/v2/catalog/templates", "get", "description"),
        "At most 100 templates are returned. Cursor pagination is not yet\n"
        "supported here; `pagination` is always the exhausted marker\n"
        "(`nextCursor: null`, `hasNextPage: false`).",
        "At most 100 templates are returned, and the response is the bare\n"
        "template list: this operation is not cursor-paginated and carries no\n"
        "`pagination` block. `GET /v2/templates` is the cursor-paged list of\n"
        "your own templates.",
    ),
]


def normalize_public_descriptions(spec: dict) -> int:
    """2026-08-28: descriptions are the published API reference.

    Drops server-internal vocabulary, the duplicated include-expansion
    sentence, and version-relative wording that ages silently.
    """
    edits = 0
    for path, old, new in DESCRIPTION_EDITS:
        current = resolve(spec, path)
        if old not in current:
            if new in current:
                continue
            raise PatchError(f"{label(path)}: upstream text changed — re-derive this edit")
        resolve(spec, path[:-1])[path[-1]] = current.replace(old, new)
        edits += 1
    return edits


# Tags an operation carries that the top-level `tags:` array never declares,
# each with the declared tag it is inserted after. Documentation renderers take
# section order and descriptions from that array, so an undeclared tag renders
# nameless and in load order.
MISSING_TOP_LEVEL_TAGS: list[tuple[str, dict[str, str]]] = [
    (
        "Pods",
        {
            "name": "Clusters",
            "description": "Multi-node cluster lifecycle and the pods that belong to a cluster.",
        },
    ),
]


def operation_tags(spec: dict) -> set[str]:
    """Every tag carried by an operation anywhere in the document."""
    used: set[str] = set()
    for path_item in resolve(spec, ("paths",)).values():
        if not isinstance(path_item, dict):
            continue
        for operation in path_item.values():
            if isinstance(operation, dict):
                used.update(operation.get("tags") or [])
    return used


def declare_operation_tags(spec: dict) -> int:
    """2026-09-17: six clusters operations carry a tag the spec never declares.

    The 2026-09-17 spec added the Clusters operations but not a Clusters entry
    to the top-level `tags:` array, which still lists eight names. Reported
    upstream with the refresh; until it lands, the published reference would
    render the clusters section without its description.
    """
    tags = resolve(spec, ("tags",))
    declared = {entry.get("name") for entry in tags if isinstance(entry, dict)}
    used = operation_tags(spec)
    edits = 0
    for after, entry in MISSING_TOP_LEVEL_TAGS:
        name = entry["name"]
        if name in declared:
            continue
        if name not in used:
            raise PatchError(f"tags: no operation carries {name!r} — retire this entry")
        position = next(
            (i for i, d in enumerate(tags) if isinstance(d, dict) and d.get("name") == after),
            None,
        )
        if position is None:
            raise PatchError(f"tags: no {after!r} entry to insert {name!r} after — repoint this entry")
        tags.insert(position + 1, copy.deepcopy(entry))
        edits += 1
    return edits


def use_neutral_secret_examples(spec: dict) -> int:
    """2026-08-28: `hunter2` is a password meme, not an example value.

    2026-09-17: upstream split ContainerConfig into BaseContainerConfig (+ registry);
    the env property and its example now live on the base schema.
    """
    env = resolve(spec, ("components", "schemas", "BaseContainerConfig", "properties", "env"))
    examples = env.get("examples")
    if examples == [{"JUPYTER_PASSWORD": "changeme"}]:
        return 0
    if examples != [{"JUPYTER_PASSWORD": "hunter2"}]:
        raise PatchError("BaseContainerConfig.env example changed upstream — re-derive this patch")
    env["examples"] = [{"JUPYTER_PASSWORD": "changeme"}]
    return 1


# Applied in order.
PATCHES = [
    ("placeholder-aws-account-ids", use_placeholder_aws_account_ids),
    ("current-brand-casing", use_current_brand_casing),
    ("enum-constant-names", name_enum_constants),
    ("public-descriptions", normalize_public_descriptions),
    ("declared-operation-tags", declare_operation_tags),
    ("neutral-secret-examples", use_neutral_secret_examples),
]


def main() -> int:
    parser = argparse.ArgumentParser(description="Apply the spec patch layer in place.")
    parser.add_argument("spec", help="Path to the OpenAPI YAML spec, updated in place")
    args = parser.parse_args()

    with open(args.spec, "r", encoding="utf-8") as fh:
        original = fh.read()

    spec = yaml.load(original, Loader=SpecLoader)
    if not isinstance(spec, dict) or "openapi" not in spec or "paths" not in spec:
        print(f"{args.spec}: not an OpenAPI document", file=sys.stderr)
        return 1

    applied = 0
    try:
        for name, patch in PATCHES:
            edits = patch(spec)
            applied += 1 if edits else 0
            print(f"{'applied' if edits else 'already applied'}: {name}")
    except PatchError as err:
        print(f"{args.spec}: {err}", file=sys.stderr)
        return 1

    text = dump_spec(spec)
    for reader in (SpecLoader, yaml.SafeLoader):
        if yaml.load(text, Loader=reader) != spec:
            print(
                f"{args.spec}: output does not round-trip through {reader.__name__}; nothing written",
                file=sys.stderr,
            )
            return 1

    if text != original:
        with open(args.spec, "w", encoding="utf-8") as fh:
            fh.write(text)

    print(f"{args.spec}: OpenAPI {spec['openapi']}, {len(spec['paths'])} paths, "
          f"{applied} of {len(PATCHES)} patches changed the document, "
          f"{'rewritten' if text != original else 'already normalized'}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
