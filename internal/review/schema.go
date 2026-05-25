package review

const (
	CurrentSchemaVersion = "review.v0.2"
	CurrentPromptVersion = "pr-review.v0.1"
)

type ModelInvocation struct {
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	PromptVersion string `json:"prompt_version"`
	InputTokens   int    `json:"input_tokens,omitempty"`
	OutputTokens  int    `json:"output_tokens,omitempty"`
	Status        string `json:"status"`
	ErrorMessage  string `json:"error_message,omitempty"`
}

func EnsureSchema(result *Result) {
	if result.SchemaVersion == "" {
		result.SchemaVersion = CurrentSchemaVersion
	}
	if result.PromptVersion == "" {
		result.PromptVersion = CurrentPromptVersion
	}
}
