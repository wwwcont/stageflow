package forms

type Flash struct {
	Kind    string
	Message string
}

type ValidationErrors map[string]string

type WorkspaceForm struct {
	ID          string
	Name        string
	Slug        string
	Description string
	OwnerTeam   string
}

type RequestForm struct {
	ID                   string
	Name                 string
	Description          string
	Method               string
	URL                  string
	HeadersText          string
	QueryText            string
	Body                 string
	Timeout              string
	FollowRedirects      bool
	RetryEnabled         bool
	RetryMaxAttempts     string
	RetryBackoffStrategy string
	RetryInitialInterval string
	RetryMaxInterval     string
	RetryStatusCodes     string
}

type FlowForm struct {
	ID          string
	Name        string
	Description string
	Status      string
}

type FlowStepForm struct {
	ID                       string
	Name                     string
	StepType                 string
	SavedRequestID           string
	InlineRequest            RequestForm
	OverrideHeadersText      string
	OverrideQueryText        string
	OverrideBody             string
	OverrideTimeout          string
	OverrideRetryEnabled     bool
	OverrideRetryMaxAttempts string
	OverrideRetryBackoff     string
	OverrideRetryInitial     string
	OverrideRetryMax         string
	OverrideRetryStatusCodes string
	ExtractionRules          []ExtractionRuleForm
	ExtractionRulesText      string
	AssertionRulesText       string
}

type ExtractionRuleForm struct {
	Name       string
	Source     string
	Selector   string
	Required   string
	JSONEncode string
}

type VariableForm struct {
	Name  string
	Value string
}

type SecretForm struct {
	Name  string
	Value string
}

type PolicyForm struct {
	AllowedHosts          string
	MaxSavedRequests      string
	MaxFlows              string
	MaxStepsPerFlow       string
	MaxRequestBodyBytes   string
	DefaultTimeoutMS      string
	MaxRunDurationSeconds string
	RetryEnabled          bool
	RetryMaxAttempts      string
	RetryBackoffStrategy  string
	RetryInitialInterval  string
	RetryMaxInterval      string
	RetryStatusCodes      string
}

type RunLaunchForm struct {
	InitiatedBy    string
	InputJSON      string
	Queue          string
	IdempotencyKey string
}

type CurlImportForm struct {
	Command string
}
