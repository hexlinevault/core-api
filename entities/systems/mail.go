package systementities

// Mail mailer struct
type Mail struct {
	Sender   string
	From     string
	Receiver string
	To       []string
	Cc       []string
	Bcc      []string
	Subject  string
	Link     string
	Body     string
	Data     interface{}
	Attach   []string
}
