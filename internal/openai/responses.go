package openai

type Request struct {
	Instructions string
	Input        string
	SchemaName   string
	Schema       map[string]any
}

type Response struct {
	Text      string
	Citations []Citation
}

type Citation struct {
	URL   string
	Title string
}

type responsesRequest struct {
	Model           string         `json:"model"`
	Instructions    string         `json:"instructions"`
	Input           string         `json:"input"`
	Reasoning       reasoning      `json:"reasoning"`
	Text            responseText   `json:"text"`
	Tools           []responseTool `json:"tools"`
	Store           bool           `json:"store"`
	MaxOutputTokens int            `json:"max_output_tokens"`
}

type reasoning struct {
	Effort string `json:"effort"`
}

type responseText struct {
	Verbosity string             `json:"verbosity"`
	Format    responseTextFormat `json:"format"`
}

type responseTextFormat struct {
	Type   string         `json:"type"`
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

type responseTool struct {
	Type string `json:"type"`
}

type responsesEnvelope struct {
	Status string `json:"status"`
	Output []struct {
		Type    string `json:"type"`
		Content []struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			Refusal     string `json:"refusal"`
			Annotations []struct {
				Type  string `json:"type"`
				URL   string `json:"url"`
				Title string `json:"title"`
			} `json:"annotations"`
		} `json:"content"`
	} `json:"output"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}
