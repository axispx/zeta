package version

// Version is set at link time via:
//
//	-ldflags "-X github.com/axispx/zeta/internal/version.Version=..."
var Version = "dev"
