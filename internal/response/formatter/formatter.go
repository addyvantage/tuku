package formatter

// ResponseFormatter controls CLI presentation style.
type ResponseFormatter interface {
	Format(text string) string
}
