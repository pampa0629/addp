package logpipeline

import (
	"strings"

	"github.com/addp/common/authorization"
)

// ObserverNode resolves the verified service client against the deployment's node list.
// The receiving process's own host is irrelevant for replicated control planes.
func ObserverNode(actor authorization.AuthContext, configuredNodes string) (string, bool) {
	if actor.Principal.Type != "service_principal" || actor.Token.Type != "service_access_token" ||
		actor.Context.Type != "platform" || actor.Delegation != nil || actor.Client.ClientID == nil {
		return "", false
	}
	for _, item := range strings.Split(configuredNodes, ",") {
		node := strings.TrimSpace(item)
		if Identity.MatchString(node) && *actor.Client.ClientID == ObserverClientID(node) {
			return node, true
		}
	}
	return "", false
}

func ObserverClientID(node string) string { return "addp-log-observer-" + node }
