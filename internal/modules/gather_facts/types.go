package gather_facts

type Request struct {
	GatherSubset interface{} `json:"gather_subset,omitempty"`
	Filter       interface{} `json:"filter,omitempty"`
	FactPath     string      `json:"fact_path,omitempty"`
}

type Response struct {
	Changed      bool                   `json:"changed"`
	Failed       bool                   `json:"failed"`
	Msg          string                 `json:"msg,omitempty"`
	AnsibleFacts map[string]interface{} `json:"ansible_facts"`
}
