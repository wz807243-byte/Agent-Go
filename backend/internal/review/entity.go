package review

type Finding struct {
	File   string `json:"file"`
	Side   string `json:"side"`
	Line   int    `json:"line"`
	Reason string `json:"reason"`
}
