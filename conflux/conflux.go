package conflux

type Conflux interface {

	// Start starts the conflux
	Start(apiBaseURL, anchorToken string, portal bool) error

	// Stop stops the conflux
	Stop()

	// StartAnchor starts the veilnet anchor
	StartAnchor(apiBaseURL, anchorToken string, portal bool) error

	// StopAnchor stops the veilnet anchor
	StopAnchor()

	// IsAnchorAlive checks if the veilnet anchor is alive
	IsAnchorAlive() bool

	// CreateTUN creates a TUN device
	CreateTUN() error

	// CloseTUN closes the TUN device
	CloseTUN() error

	// DetectHostGateway detects the host default gateway and interface
	DetectHostGateway() error

	// AddBypassRoutes adds bypass routes
	AddBypassRoutes()

	// RemoveBypassRoutes removes bypass routes
	RemoveBypassRoutes()
}

func NewConflux() Conflux {
	return newConflux()
}
