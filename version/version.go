package version

const (
	// Version is the Operator's version
	Version = "0.11.0-beta.2"

	// AstarteVersionConstraintString represents the range of supported Astarte versions for this Operator.
	// If the Astarte version falls out of this range, reconciliation will be immediately aborted.
	AstarteVersionConstraintString = ">= 0.10.0, < 0.12.0"
)
