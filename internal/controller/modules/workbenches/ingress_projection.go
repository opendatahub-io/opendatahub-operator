package workbenches

// IngressContractVersion is the contract version the platform projects onto
// the Workbenches module CR. The consumer (workbenches-operator) validates it
// and gates its own ConfigMap rendering on the reported rollout state.
const IngressContractVersion = "v1"
