package monitor

// Render implements the Renderer interface for JSONRenderer.
func (r *JSONRenderer) Render(state *MonitorState) string {
	return encodeJSONState(state, r.Pretty)
}
