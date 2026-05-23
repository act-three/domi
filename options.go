package domi

// An Option configures a [Handler].
type Option interface {
	isOption()
}

type handlerConfig struct {
	document func(title string, body Node) Node
}

// resolveOptions folds opts into a single handlerConfig.
func resolveOptions(opts []Option) handlerConfig {
	config := handlerConfig{
		document: defaultDocument,
	}
	for _, o := range opts {
		switch o := o.(type) {
		case documentOption:
			config.document = o.f
		}
	}
	return config
}

// Document supplies a custom builder for the initial HTML shell.
// The builder is called once per session
// with the initial document title and the body element.
// It is responsible for returning a complete html element.
// The framework always writes the HTML5 doctype declaration
// before the html element.
//
//	domi.Handler(newApp, domi.Document(func(title string, body domi.Node) domi.Node {
//	    return html.HTML()(
//	        html.Head()(
//	            html.Meta(attr.Charset("utf-8")),
//	            html.Title()(domi.Text(title)),
//	            html.Script(attr.Type("module"), attr.Src("/bundle.js")),
//	        ),
//	        body,
//	    )
//	}))
//
// Apps using Document are responsible for loading the Domi client JavaScript.
// See [Bundling the Client] for details.
func Document(f func(title string, body Node) Node) Option {
	return documentOption{f}
}

type documentOption struct {
	f func(title string, body Node) Node
}

func (documentOption) isOption() {}
