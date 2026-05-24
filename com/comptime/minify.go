package main

import (
	"io"
	"os"
	"path/filepath"

	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/css"
	"github.com/tdewolff/minify/v2/html"
	"github.com/tdewolff/minify/v2/js"
)

type Asset struct {
	In   string
	Out  string
	Mime string
}

func main() {
	m := minify.New()
	const thtml = "text/html"
	const tcss = "text/css"
	const tjs = "application/javascript"

	m.AddFunc("text/html", html.Minify)
	m.AddFunc("text/css", css.Minify)
	m.AddFunc("application/javascript", js.Minify)

	os.Mkdir("web", 0755)
	os.Mkdir("web/js", 0755)
	os.Mkdir("web/html", 0755)
	os.Mkdir("web/html/partials", 0755)
	os.Mkdir("web/css", 0755)

	os.CopyFS("web/image", os.DirFS("public/image"))

	//HTML
	process(m, Asset{In: "public/html/index.html", Out: "web/html/index.html", Mime: thtml})
	process(m, Asset{In: "public/html/about_editor.html", Out: "web/html/about_editor.html", Mime: thtml})
	process(m, Asset{In: "public/html/admin-center.html", Out: "web/html/admin-center.html", Mime: thtml})
	process(m, Asset{In: "public/html/data.html", Out: "web/html/data.html", Mime: thtml})
	process(m, Asset{In: "public/html/gallery.html", Out: "web/html/gallery.html", Mime: thtml})
	process(m, Asset{In: "public/html/local_about.html", Out: "web/html/local_about.html", Mime: thtml})
	process(m, Asset{In: "public/html/local.html", Out: "web/html/local.html", Mime: thtml})
	process(m, Asset{In: "public/html/login.html", Out: "web/html/login.html", Mime: thtml})
	process(m, Asset{In: "public/html/message_viewer.html", Out: "web/html/message_viewer.html", Mime: thtml})
	process(m, Asset{In: "public/html/messages.html", Out: "web/html/messages.html", Mime: thtml})
	process(m, Asset{In: "public/html/satdump.html", Out: "web/html/satdump.html", Mime: thtml})
	process(m, Asset{In: "public/html/stats.html", Out: "web/html/stats.html", Mime: thtml})
	process(m, Asset{In: "public/html/template_editor.html", Out: "web/html/template_editor.html", Mime: thtml})
	//Partials
	process(m, Asset{In: "public/html/partials/admin-gen.html", Out: "web/html/partials/admin-gen.html", Mime: thtml})
	process(m, Asset{In: "public/html/partials/admin-img.html", Out: "web/html/partials/admin-img.html", Mime: thtml})
	process(m, Asset{In: "public/html/partials/admin-net.html", Out: "web/html/partials/admin-net.html", Mime: thtml})
	process(m, Asset{In: "public/html/partials/admin-pss.html", Out: "web/html/partials/admin-pss.html", Mime: thtml})
	process(m, Asset{In: "public/html/partials/admin-sat.html", Out: "web/html/partials/admin-sat.html", Mime: thtml})
	process(m, Asset{In: "public/html/partials/admin-stg.html", Out: "web/html/partials/admin-stg.html", Mime: thtml})
	process(m, Asset{In: "public/html/partials/advanced-view.html", Out: "web/html/partials/advanced-view.html", Mime: thtml})
	process(m, Asset{In: "public/html/partials/simplified-view.html", Out: "web/html/partials/simplified-view.html", Mime: thtml})
	//JS
	process(m, Asset{In: "public/js/advanced-view.js", Out: "web/js/advanced-view.js", Mime: tjs})
	process(m, Asset{In: "public/js/data.js", Out: "web/js/data.js", Mime: tjs})
	process(m, Asset{In: "public/js/home.js", Out: "web/js/home.js", Mime: tjs})
	process(m, Asset{In: "public/js/local_about.js", Out: "web/js/local_about.js", Mime: tjs})
	process(m, Asset{In: "public/js/message_viewer.js", Out: "web/js/message_viewer.js", Mime: tjs})
	process(m, Asset{In: "public/js/messages.js", Out: "web/js/messages.js", Mime: tjs})
	process(m, Asset{In: "public/js/satdump.js", Out: "web/js/satdump.js", Mime: tjs})
	process(m, Asset{In: "public/js/shared-view.js", Out: "web/js/shared-view.js", Mime: tjs})
	process(m, Asset{In: "public/js/simplified-view.js", Out: "web/js/simplified-view.js", Mime: tjs})
	process(m, Asset{In: "public/js/stats.js", Out: "web/js/stats.js", Mime: tjs})
	process(m, Asset{In: "public/js/template_editor.js", Out: "web/js/template_editor.js", Mime: tjs})
	//CSS
	process(m, Asset{In: "public/css/data.css", Out: "web/css/data.css", Mime: tcss})
	process(m, Asset{In: "public/css/gallery.css", Out: "web/css/gallery.css", Mime: tcss})
	process(m, Asset{In: "public/css/home.css", Out: "web/css/home.css", Mime: tcss})
	process(m, Asset{In: "public/css/styles.css", Out: "web/css/styles.css", Mime: tcss})
	process(m, Asset{In: "public/css/template_editor.css", Out: "web/css/template_editor.css", Mime: tcss})

}

func process(m *minify.M, a Asset) error {
	if err := os.MkdirAll(filepath.Dir(a.Out), 0755); err != nil {
		return err
	}

	in, err := os.Open(a.In)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(a.Out)
	if err != nil {
		return err
	}
	defer out.Close()

	if a.Mime == "" {
		_, err = io.Copy(out, in)
		return err
	}

	return m.Minify(a.Mime, out, in)
}
