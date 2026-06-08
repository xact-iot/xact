package backups

type Schema struct {
	Tables map[string]Table `json:"tables"`
}

type Table struct {
	Columns    []Column       `json:"columns"`
	PrimaryKey []string       `json:"primary_key,omitempty"`
	Indexes    []Index        `json:"indexes,omitempty"`
	Extensions map[string]any `json:"extensions,omitempty"`
}

type Column struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
}

type Index struct {
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique"`
}
