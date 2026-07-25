package image

// Ref is an image embedded as a data: URL (session + prompt history + API).
// No separate on-disk attachment store — OpenCode-style.
type Ref struct {
	URL  string `json:"url"`            // data:<mime>;base64,...
	MIME string `json:"mime"`           // e.g. image/png
	Name string `json:"name,omitempty"` // display name (basename)
}
