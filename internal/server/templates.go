package server

import (
	"embed"
	"html/template"
)

//go:embed templates/*.html
var templatesFS embed.FS

var downloadTemplate *template.Template
var uploadTemplate *template.Template

func init() {
	downloadTemplate = template.Must(template.ParseFS(templatesFS, "templates/download.html"))
	uploadTemplate = template.Must(template.ParseFS(templatesFS, "templates/upload.html"))
}
