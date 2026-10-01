package version

const Name = "runhug"

// Version is the SemVer string for this build. Overridable at link time:
//
//	-ldflags "-X github.com/adamsiwiec1/runhug/internal/version.Version=0.1.7"
var Version = "0.3.2"
