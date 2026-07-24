package nagent

type Intent struct {
	IntentID          string `json:"intentId"`
	Issuer            string `json:"issuer"`
	IntentPriority    int    `json:"intentPriority"`
	IntentType        string `json:"intentType"`
	IntentDescription string `json:"intentDescription"`
	Object            string `json:"object"`
	Constraint        string `json:"constraint"`
	Target            string `json:"target"`
}

type AgentRoute struct {
	Name        string
	BaseURI     string
	Path        string
	Schema      string
	IntentTypes []string
}
