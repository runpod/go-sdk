import * as runpod from "@pulumi/runpod";

// const random = new runpod.Provider("my-random", { token: "" });

const pod = new runpod.Pod("my-test-pod", {
  gpuTypeId: "A6000",
  gpuCount: 2,
  cloudType: "ALL",
});

export const output = pod.gpuCount;
