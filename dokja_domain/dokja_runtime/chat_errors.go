package runtime

// ChatFailureCode describes the observed failure, never a reason to retry or
// switch providers automatically. Infrastructure maps private diagnostics to
// these categories; only fixed messages leave the inference boundary.
type ChatFailureCode string

const (
	ChatSessionLimit     ChatFailureCode = "session_limit"
	ChatUsageLimit       ChatFailureCode = "usage_limit"
	ChatAuthentication   ChatFailureCode = "authentication_required"
	ChatModelUnavailable ChatFailureCode = "model_unavailable"
	ChatCLIIncompatible  ChatFailureCode = "cli_incompatible"
	ChatOutputLimit      ChatFailureCode = "output_limit"
	ChatUnavailable      ChatFailureCode = "unavailable"
)

type ChatProviderFailure struct{ Code ChatFailureCode }

func (e *ChatProviderFailure) Error() string {
	switch e.Code {
	case ChatSessionLimit:
		return "The selected chat provider reports a session limit. Check its session allowance before retrying."
	case ChatUsageLimit:
		return "The selected chat provider reports a usage or rate limit. Check its allowance before retrying."
	case ChatAuthentication:
		return "The selected chat provider requires authentication. Sign in using its host CLI, then retry."
	case ChatModelUnavailable:
		return "The selected chat model is unavailable to this provider. Review the model selection and provider access."
	case ChatCLIIncompatible:
		return "The installed chat provider CLI rejected the runtime invocation. Check its version and compatibility."
	case ChatOutputLimit:
		return "The selected chat provider exceeded the supported output size."
	default:
		return "The selected chat provider is unavailable or inference failed."
	}
}
