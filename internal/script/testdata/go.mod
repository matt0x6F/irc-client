module cascade-scripts

go 1.21

// Keep this version equal to cascadeSDKVersion in ../manager.go: tests run
// LoadAll against this dir, and reconcileModule rewrites the line when it
// differs (which would dirty this tracked fixture). Bump both together.
require github.com/matt0x6f/irc-client/cascade v1.2.0
