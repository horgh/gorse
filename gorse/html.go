package main

import (
	"errors"
	"html/template"
	"log"
	"net/http"
	"regexp"
)

// renderPage builds a full page. the specified content template is
// used to build the content section of the page wrapped between
// header and footer.
func renderPage(rw http.ResponseWriter, contentTemplate string,
	data interface{}) error {
	// ensure the specified content template is valid.
	matched, err := regexp.MatchString("^[_a-zA-Z]+$", contentTemplate)
	if err != nil || !matched {
		return errors.New("Invalid template name")
	}

	header, err := template.ParseFiles("templates/_header.html")
	if err != nil {
		log.Printf("Failed to load header: %s", err)
		return err
	}

	// content.
	funcMap := template.FuncMap{
		"getRowCSSClass": getRowCSSClass,
	}
	// we need the base path as that is the name that gets assigned
	// to the template internally due to how we create the template.
	// that is, through New(), then ParseFiles() - ParseFiles() sets
	// the name of the template using the basename of the file.
	contentTemplateBasePath := contentTemplate + ".html"
	contentTemplatePath := "templates/" + contentTemplateBasePath
	content, err := template.New("content").Funcs(funcMap).ParseFiles(contentTemplatePath)
	if err != nil {
		log.Printf("Failed to load content template [%s]: %s", contentTemplate, err)
		return err
	}

	// footer.
	footer, err := template.ParseFiles("templates/_footer.html")
	if err != nil {
		log.Printf("Failed to load footer: %s", err)
		return err
	}

	// execute the templates and write them out.
	err = header.Execute(rw, data)
	if err != nil {
		log.Printf("Failed to execute header: %s", err)
		return err
	}
	err = content.ExecuteTemplate(rw, contentTemplateBasePath, data)
	if err != nil {
		log.Printf("Failed to execute content: %s", err)
		return err
	}
	err = footer.Execute(rw, data)
	if err != nil {
		log.Printf("Failed to execute footer: %s", err)
		return err
	}
	return nil
}

// getRowCSSClass takes a row index and determines the css class to use.
func getRowCSSClass(index int) string {
	if index%2 == 0 {
		return "row1"
	}
	return "row2"
}

// getHTMLDescription builds the HTML encoded description.
// we call it while generating HTML.
// text is the unencoded string, and we return HTML encoded.
// we have this so we can make inline urls into links.
func getHTMLDescription(text string) template.HTML {
	// encode the entire string as HTML first.
	html := template.HTMLEscapeString(text)

	// wrap up URLs in <a>.
	// I previously used this re: \b(https?://\S+)
	// but there were issues with it recognising non-url characters. I even
	// found it match a space which seems like it should be impossible.
	re := regexp.MustCompile(`\b(https?://[A-Za-z0-9\-\._~:/\?#\[\]@!\$&'\(\)\*\+,;=]+)`)
	return template.HTML(re.ReplaceAllString(html, `<a href="$1">$1</a>`))
}
