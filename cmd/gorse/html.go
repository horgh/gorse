package main

import (
	"errors"
	"html"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
)

// renderPage builds a full page.
//
// The specified content template is used to build the content section of the
// page wrapped between header and footer.
func renderPage(settings *Config, rw http.ResponseWriter,
	contentTemplate string, data interface{}) error {
	// Ensure the specified content template is valid.
	matched, err := regexp.MatchString("^[_a-zA-Z]+$", contentTemplate)
	if err != nil || !matched {
		return errors.New("invalid template name")
	}

	header, err := template.ParseFiles(
		filepath.Join(settings.TemplateDir, "_header.html"))
	if err != nil {
		log.Printf("Failed to load header: %s", err)
		return err
	}

	// Content.

	funcMap := template.FuncMap{
		"getRowCSSClass": getRowCSSClass,
	}

	// We need the base path as that is the name that gets assigned to the
	// template internally due to how we create the template. That is, through
	// New(), then ParseFiles() - ParseFiles() sets the name of the template
	// using the basename of the file.
	contentTemplateBasePath := contentTemplate + ".html"
	contentTemplatePath := filepath.Join(settings.TemplateDir,
		contentTemplateBasePath)
	content, err := template.New("content").Funcs(funcMap).ParseFiles(
		contentTemplatePath)
	if err != nil {
		log.Printf("Failed to load content template [%s]: %s", contentTemplate, err)
		return err
	}

	// Footer.
	footer, err := template.ParseFiles(
		filepath.Join(settings.TemplateDir, "_footer.html"))
	if err != nil {
		log.Printf("Failed to load footer: %s", err)
		return err
	}

	// Execute the templates and write them out.

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
//
// We call this while generating HTML.
//
// Text is the unencoded string, and we return HTML encoded.
//
// We have this so we can make inline URLs into links.
func getHTMLDescription(text string) template.HTML {
	// Encode the entire string as HTML first.
	html := template.HTMLEscapeString(text)

	// Wrap up URLs in <a>.
	//
	// I previously used this re: \b(https?://\S+)
	//
	// But there were issues with it recognising non-URL characters. I even found
	// it match a space which seems like it should be impossible.
	re := regexp.MustCompile(`\b(https?://[A-Za-z0-9\-\._~:/\?#\[\]@!\$&'\(\)\*\+,;=]+)`)
	return template.HTML(re.ReplaceAllString(html, `<a href="$1">$1</a>`))
}

// sanitiseItemText takes text (e.g., title or description) and removes any HTML
// markup. This is because some feeds (e.g., Slashdot) include a lot of markup
// I don't want to actually show.
//
// We also decode HTML entities since apparently we can get these through to
// this point (they will be encoded again as necessary when we render the
// page).
//
// For example in a raw XML from Slashdot we have this:
//
// <item><title>AT&amp;amp;T Gets Patent To Monitor and Track File-Sharing Traffic</title>
//
// Which gets placed into the database as:
// AT&amp;T Gets Patent To Monitor and Track File-Sharing Traffic
//
// This can be used to take any string which has HTML in it to clean up that
// string and make it non-HTML.
//
// While elements such as 'title' can have HTMLin them, this seems applied
// inconsistently. For instance, consider this title from a Slashdot feed:
//
// <title>Google Maps Updated With Skyfall&lt;/em&gt; Island Japan Terrain</title>
//
// That is: </em> in there but no <em>.
//
// In the database this is present as </em>.
//
// Thus we do not place the HTML into the page raw.
func sanitiseItemText(text string) (string, error) {
	// First remove raw HTML.
	re, err := regexp.Compile("(?s)<.*?>")
	if err != nil {
		log.Printf("Failed to compile html regexp: %s", err)
		return text, err
	}
	text = re.ReplaceAllString(text, "")

	// Decode HTML entities.
	text = html.UnescapeString(text)

	// Turn any multiple spaces into a single space.
	re, err = regexp.Compile("(?s)\\s+")
	if err != nil {
		log.Printf("Failed to compile whitespace regexp: %s", err)
		return text, err
	}
	text = re.ReplaceAllString(text, " ")

	return text, nil
}
