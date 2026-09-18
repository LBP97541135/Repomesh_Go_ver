package scan

// DefaultChannels returns the built-in channel table in scan order:
// BUILD, RUNTIME_CALL, SHARED_RESOURCE, DEPLOY, SOURCE, API routes.
// Registration is the extension point — a new channel is registered, not
// coded into the walker.
func DefaultChannels() []EvidenceChannel {
	return []EvidenceChannel{
		NewBuildChannel(),
		NewRuntimeCallChannel(),
		NewResourceChannel(),
		NewDeployChannel(),
		NewSourceChannel(),
		NewAPIRouteChannel(),
	}
}
