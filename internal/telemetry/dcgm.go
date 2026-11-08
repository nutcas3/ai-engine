package telemetry

// XidAction maps an NVIDIA Xid error code to a remediation action. Based on
// the NVIDIA GPU node triage guide and the EKS node monitoring agent policy.
type XidAction string

const (
	XidIgnore     XidAction = "ignore"       // informational only
	XidDrainReset XidAction = "drain_reset"  // cordon, drain, GPU reset
	XidReplace    XidAction = "replace"      // cordon, drain, CAPI replaces node
	XidReboot     XidAction = "reboot"       // recoverable after reboot
)

// XidTable classifies the most operationally significant Xid codes.
var XidTable = map[int]XidAction{
	13: XidDrainReset, // Graphics Engine Exception — run diagnostics
	31: XidReplace,    // Suspected hardware problem — vendor diagnostics/RMA
	45: XidIgnore,     // Robust Channel Preemptive Removal — informative
	48: XidDrainReset, // Double-bit ECC error — drain + reset, may escalate to RMA
	61: XidDrainReset, // PMU breakpoint — reset GPU
	62: XidDrainReset, // PMU halt — reset GPU
	63: XidIgnore,     // Row-remapping record — keep running until convenient reboot
	74: XidReplace,    // NVLink error — replace
	79: XidReplace,    // GPU fell off the bus — hardware failure
	94: XidDrainReset, // Contained ECC error — drain app, reset
	95: XidReplace,    // Uncontained error — replace
}

// Classify returns the remediation action for an Xid code; unknown codes are
// logged but do not trigger automated remediation.
func Classify(xid int) XidAction {
	if a, ok := XidTable[xid]; ok {
		return a
	}
	return XidIgnore
}

