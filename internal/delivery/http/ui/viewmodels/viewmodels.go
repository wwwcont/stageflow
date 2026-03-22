package viewmodels

import "stageflow/internal/delivery/http/ui/forms"

type Breadcrumb struct {
	Label string
	URL   string
}

type NavItem struct {
	Label  string
	URL    string
	Active bool
}

type LanguageOption struct {
	Code   string
	Label  string
	URL    string
	Active bool
}

type WorkspaceTab struct {
	Label  string
	URL    string
	Active bool
}

type Page struct {
	Title           string
	Description     string
	Lang            string
	CurrentPath     string
	Breadcrumbs     []Breadcrumb
	PrimaryNav      []NavItem
	LanguageOptions []LanguageOption
	WorkspaceTabs   []WorkspaceTab
	Flash           *forms.Flash
	Errors          forms.ValidationErrors
	HasAutoRefresh  bool
	AutoRefreshURL  string
}

type WorkspaceListItem struct {
	ID          string
	Name        string
	Slug        string
	Description string
	OwnerTeam   string
	Status      string
	UpdatedAt   string
}

type WorkspaceOverviewStats struct {
	SavedRequests int
	Flows         int
	Runs          int
}

type SavedRequestListItem struct {
	ID          string
	Name        string
	Description string
	Method      string
	URL         string
	Timeout     string
	UpdatedAt   string
}

type FlowListItem struct {
	ID          string
	Name        string
	Description string
	Status      string
	Version     int
	StepCount   int
	UpdatedAt   string
}

type FlowStepListItem struct {
	ID             string
	OrderIndex     int
	Name           string
	StepType       string
	SavedRequestID string
	Summary        string
}

type SecretListItem struct {
	Name string
}

type VariableListItem struct {
	Name  string
	Value string
}

type RunListItem struct {
	ID          string
	Status      string
	TargetType  string
	TargetName  string
	StartedAt   string
	FinishedAt  string
	Duration    string
	InitiatedBy string
}

type RunStepItem struct {
	Name             string
	Status           string
	RetryCount       int
	Duration         string
	ErrorMessage     string
	RequestSnapshot  string
	ResponseSnapshot string
	ExtractedValues  string
	Attempts         string
}

type RunEventItem struct {
	Sequence   int64
	Timestamp  string
	Level      string
	EventType  string
	StepLabel  string
	Message    string
	Details    string
	HasDetails bool
}

type HomePage struct {
	Page       Page
	Workspaces []WorkspaceListItem
}

type WorkspaceListPage struct {
	Page       Page
	Workspaces []WorkspaceListItem
}

type WorkspaceFormPage struct {
	Page        Page
	Form        forms.WorkspaceForm
	SubmitURL   string
	SubmitLabel string
	WorkspaceID string
	IsEdit      bool
}

type WorkspaceOverviewPage struct {
	Page      Page
	Workspace WorkspaceListItem
	Policy    map[string]string
	Stats     WorkspaceOverviewStats
}

type SavedRequestListPage struct {
	Page      Page
	Workspace WorkspaceListItem
	Requests  []SavedRequestListItem
}

type SavedRequestFormPage struct {
	Page        Page
	Workspace   WorkspaceListItem
	Form        forms.RequestForm
	SubmitURL   string
	SubmitLabel string
	IsEdit      bool
}

type SavedRequestDetailsPage struct {
	Page      Page
	Workspace WorkspaceListItem
	Request   SavedRequestListItem
	Form      forms.RunLaunchForm
	Spec      map[string]string
}

type CurlImportPage struct {
	Page      Page
	Workspace WorkspaceListItem
	Form      forms.CurlImportForm
	Preview   *forms.RequestForm
}

type FlowStepCurlImportPage struct {
	Page      Page
	Workspace WorkspaceListItem
	Flow      FlowListItem
	Form      forms.CurlImportForm
	Preview   *forms.FlowStepForm
}

type FlowListPage struct {
	Page      Page
	Workspace WorkspaceListItem
	Flows     []FlowListItem
}

type FlowFormPage struct {
	Page             Page
	Workspace        WorkspaceListItem
	Form             forms.FlowForm
	SubmitURL        string
	SubmitLabel      string
	IsEdit           bool
	Steps            []FlowStepListItem
	ValidationIssues []string
}

type FlowDetailsPage struct {
	Page      Page
	Workspace WorkspaceListItem
	Flow      FlowListItem
	Steps     []FlowStepListItem
	RunForm   forms.RunLaunchForm
}

type FlowStepFormPage struct {
	Page          Page
	Workspace     WorkspaceListItem
	Flow          FlowListItem
	Form          forms.FlowStepForm
	SubmitURL     string
	SubmitLabel   string
	IsEdit        bool
	SavedRequests []SavedRequestListItem
}

type VariablesPage struct {
	Page      Page
	Workspace WorkspaceListItem
	Variables []VariableListItem
}

type VariableFormPage struct {
	Page        Page
	Workspace   WorkspaceListItem
	Form        forms.VariableForm
	SubmitURL   string
	SubmitLabel string
	IsEdit      bool
	OriginalKey string
}

type SecretsPage struct {
	Page      Page
	Workspace WorkspaceListItem
	Secrets   []SecretListItem
}

type SecretFormPage struct {
	Page        Page
	Workspace   WorkspaceListItem
	Form        forms.SecretForm
	SubmitURL   string
	SubmitLabel string
}

type PolicyPage struct {
	Page      Page
	Workspace WorkspaceListItem
	Form      forms.PolicyForm
}

type RunsPage struct {
	Page      Page
	Workspace WorkspaceListItem
	Runs      []RunListItem
}

type DocsPage struct {
	Page Page
}

type RunDetailsPage struct {
	Page              Page
	Workspace         WorkspaceListItem
	Run               RunListItem
	ErrorSummary      string
	InputJSON         string
	RunSteps          []RunStepItem
	TargetDetails     map[string]string
	RunEvents         []RunEventItem
	LastEventSequence int64
	EventsStreamURL   string
	StreamState       string
}
