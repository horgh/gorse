//
// gorse is a web front end to a database of RSS feeds/items. The database gets
// populated by my RSS poller, gorsepoll.
//
// The interface shows items from feeds and allows flagging them as read.
//
// For the database schema, refer to gorsepoll.
//
package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"log"
	"math"
	"net"
	"net/http"
	"net/http/fcgi"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/gorilla/context"
	"github.com/gorilla/sessions"
	_ "github.com/lib/pq"
	"summercat.com/config"
	"summercat.com/gorse/gorselib"
)

// GorseConfig holds runtime configuration information.
type GorseConfig struct {
	ListenHost string
	ListenPort uint64
	DBUser     string
	DBPass     string
	DBName     string
	DBHost     string

	// TODO: Auto detect timezone, or move this to a user setting
	DisplayTimeZone string

	URIPrefix               string
	CookieAuthenticationKey string
	SessionName             string
}

// DB is the connection to the database.
// This is so we try to share a single connection for multiple requests.
// NOTE: According to the database/sql documentation, the DB type
//   is indeed safe for concurrent use by multiple goroutines.
var DB *sql.DB

// HTTPHandler holds functions/data used to service HTTP requests.
// We need this struct as we must pass instances of it to fcgi.Serve.
// This is because it must conform to the http.Handler interface.
type HTTPHandler struct {
	settings     *GorseConfig
	sessionStore *sessions.CookieStore
}

type sortOrder int

const (
	sortAscending sortOrder = iota
	sortDescending
)

const pageSize = 50

// connectToDB opens a new connection to the database.
func connectToDB(settings *GorseConfig) (*sql.DB, error) {
	dsn := fmt.Sprintf("user=%s password=%s dbname=%s host=%s connect_timeout=10",
		settings.DBUser, settings.DBPass, settings.DBName, settings.DBHost)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Print("Failed to connect to the database: " + err.Error())
		return nil, err
	}

	log.Print("Opened new connection to the database.")
	return db, nil
}

// getDB connects us to the database if necessary, and returns an active
// database connection.
// we use the global DB variable to try to ensure we use a single connection.
func getDB(settings *GorseConfig) (*sql.DB, error) {
	// If we have a db connection, ensure that it is still available so that we
	// reconnect if it is not.
	if DB != nil {
		err := DB.Ping()
		if err == nil {
			return DB, nil
		}

		log.Printf("Database ping failed: %s", err)

		// Continue on, but set us so that we attempt to reconnect.
		// TODO: Race condition
		_ = DB.Close()
		DB = nil
	}

	db, err := connectToDB(settings)
	if err != nil {
		log.Printf("Failed to connect to the database: %s", err)
		return nil, err
	}

	// Set global
	// TODO: Race condition
	DB = db

	return DB, nil
}

// setItemRead sets the given item read in the database.
func setItemRead(db *sql.DB, id int64) error {
	query := `
UPDATE rss_item
SET read = true
WHERE id = $1
`
	_, err := db.Exec(query, id)
	if err != nil {
		log.Printf("Failed to set item id [%d] read: %s", id, err.Error())
		return err
	}
	log.Printf("Set item id [%d] read", id)
	return nil
}

// countTotalItems retrieves a count of unread feed items.
//
// This is for pagination.
func countTotalItems(db *sql.DB) (int, error) {
	query := `
SELECT COUNT(1) AS count
FROM rss_item ri
LEFT JOIN rss_feed rf ON rf.id = ri.rss_feed_id
WHERE rf.active = true
	AND ri.read = false
`

	rows, err := db.Query(query)
	if err != nil {
		return -1, err
	}

	if !rows.Next() {
		return -1, errors.New("Count not found")
	}

	var count int
	err = rows.Scan(&count)
	if err != nil {
		_ = rows.Close()
		return -1, err
	}

	err = rows.Close()
	if err != nil {
		return -1, fmt.Errorf("Problem closing rows: %s", err)
	}

	return count, nil
}

// retrieveFeedItems retrieves feed items from the database which are
// marked non-read.
func retrieveFeedItems(db *sql.DB, settings *GorseConfig, order sortOrder,
	page int) ([]gorselib.RssItem, error) {

	if page < 1 {
		return nil, errors.New("Invalid page number.")
	}

	query := `
SELECT
rf.name, ri.id, ri.title, ri.link, ri.description, ri.publication_date
FROM rss_item ri
LEFT JOIN rss_feed rf ON rf.id = ri.rss_feed_id
WHERE rf.active = true
	AND ri.read = false
`

	if order == sortAscending {
		query += "ORDER BY ri.publication_date ASC"
	} else {
		query += "ORDER BY ri.publication_date DESC"
	}

	query += " LIMIT $1 OFFSET $2"

	offset := (page - 1) * pageSize

	rows, err := db.Query(query, pageSize, offset)
	if err != nil {
		return nil, err
	}

	// our display timezone location.
	location, err := time.LoadLocation(settings.DisplayTimeZone)
	if err != nil {
		log.Printf("Failed to load time zone location [%s]",
			settings.DisplayTimeZone)
		return nil, err
	}

	var items []gorselib.RssItem
	for rows.Next() {
		var item gorselib.RssItem
		err := rows.Scan(&item.FeedName, &item.Id, &item.Title, &item.Uri,
			&item.Description, &item.PublicationDate)
		if err != nil {
			log.Printf("Failed to scan row information: %s", err.Error())
			return nil, err
		}

		// set time to the display timezone.
		item.PublicationDate = item.PublicationDate.In(location)

		// sanitise the text.
		item.Title, err = gorselib.SanitiseItemText(item.Title)
		if err != nil {
			log.Printf("Failed to sanitise title: %s", err.Error())
			return nil, err
		}
		item.Description, err = gorselib.SanitiseItemText(item.Description)
		if err != nil {
			log.Printf("Failed to sanitise description: %s", err.Error())
			return nil, err
		}

		items = append(items, item)
	}

	return items, nil
}

// send500Error sends an internal server error with the given message in the
// body.
func send500Error(rw http.ResponseWriter, message string) {
	rw.WriteHeader(http.StatusInternalServerError)
	_, _ = rw.Write([]byte("<h1>" + template.HTMLEscapeString(message) + "</h1>"))
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
		log.Printf("Failed to load header: %s", err.Error())
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
		log.Printf("Failed to load content template [%s]: %s",
			contentTemplate, err.Error())
		return err
	}

	// footer.
	footer, err := template.ParseFiles("templates/_footer.html")
	if err != nil {
		log.Printf("Failed to load footer: %s", err.Error())
		return err
	}

	// execute the templates and write them out.
	err = header.Execute(rw, data)
	if err != nil {
		log.Printf("Failed to execute header: %s", err.Error())
		return err
	}
	err = content.ExecuteTemplate(rw, contentTemplateBasePath, data)
	if err != nil {
		log.Printf("Failed to execute content: %s", err.Error())
		return err
	}
	err = footer.Execute(rw, data)
	if err != nil {
		log.Printf("Failed to execute footer: %s", err.Error())
		return err
	}
	return nil
}

// handlerListItems handles a list rss items request and builds an html
// response.
// it implements the type RequestHandlerFunc
func handlerListItems(rw http.ResponseWriter, request *http.Request,
	settings *GorseConfig, session *sessions.Session) {
	db, err := getDB(settings)
	if err != nil {
		log.Printf("Failed to get database connection: %s", err)
		send500Error(rw, "Failed to connect to database")
		return
	}

	// Retrieve the feeds from the database. we want to be able to
	// list our feeds and show information such as when the last time
	// we updated was.
	feeds, err := gorselib.RetrieveFeeds(db)
	if err != nil {
		log.Printf("Failed to retrieve feeds: %s", err.Error())
		send500Error(rw, "Failed to retrieve feeds")
		return
	}

	// We can be told different sort display order. This is in the URL.
	requestValues := request.URL.Query()

	// Default is date descending.
	order := sortDescending
	reverseSortOrder := "date-asc"

	sortRaw := requestValues.Get("sort-order")
	if sortRaw == "" || sortRaw == "date-desc" {
		order = sortDescending
		reverseSortOrder = "date-asc"
	}
	if sortRaw == "date-asc" {
		order = sortAscending
		reverseSortOrder = "date-desc"
	}

	page := 1
	pageParam := requestValues.Get("page")
	if pageParam != "" {
		page, err = strconv.Atoi(pageParam)
		if err != nil {
			page = 1
		}
	}

	// Retrieve items from the database.
	items, err := retrieveFeedItems(db, settings, order, page)
	if err != nil {
		log.Printf("Failed to retrieve items: %s", err.Error())
		send500Error(rw, "Failed to retrieve items")
		return
	}

	// TODO: move this to be calculated by a method?
	//   may also be able to move some of the post processing on items
	//   in retrieveFeedItems() into methods.
	// set up additional information about each item.
	// specifically we want to set a string timestamp.
	for i, item := range items {
		// format time.
		items[i].PublicationDateString = item.PublicationDate.Format(time.RFC1123Z)

		// ensure we say no title if there is no title.
		// (so there is something to have in the link content)
		if len(items[i].Title) == 0 {
			items[i].Title = "<No title>"
		}

		// make HTML version of description. we set it as type HTML so the template
		// execution knows not to re-encode it. we want to control the encoding
		// more carefully for making links of URLs, for one.
		items[i].DescriptionHTML = getHTMLDescription(items[i].Description)
	}

	// Get count of total feed items (all pages).
	totalItems, err := countTotalItems(db)
	if err != nil {
		log.Printf(err.Error())
		send500Error(rw, "Failed to lookup count.")
		return
	}

	totalPages := int(math.Ceil(float64(totalItems) / float64(pageSize)))
	nextPage := -1
	if page < totalPages {
		nextPage = page + 1
	}
	prevPage := page - 1

	// We may have messages to display.
	// Right now we only have success messages
	flashes := session.Flashes()
	var successMessages []string
	for _, flash := range flashes {
		// type assertion. flash is interface{}
		if str, ok := flash.(string); ok {
			successMessages = append(successMessages, str)
		}
	}

	err = session.Save(request, rw)
	if err != nil {
		log.Printf("Unable to save session: %s", err)
		send500Error(rw, "Failed to save your session.")
		return
	}

	// Show the page.

	type ListItemsPage struct {
		PageTitle        string
		Items            []gorselib.RssItem
		Feeds            []gorselib.RssFeed
		SuccessMessages  []string
		Path             string
		SortOrder        string
		ReverseSortOrder string
		TotalItems       int
		Page             int
		NextPage         int
		PreviousPage     int
	}

	listItemsPage := ListItemsPage{
		PageTitle:        "",
		Items:            items,
		Feeds:            feeds,
		SuccessMessages:  successMessages,
		Path:             request.URL.Path,
		SortOrder:        sortRaw,
		ReverseSortOrder: reverseSortOrder,
		TotalItems:       totalItems,
		Page:             page,
		NextPage:         nextPage,
		PreviousPage:     prevPage,
	}

	err = renderPage(rw, "_list_items", listItemsPage)
	if err != nil {
		log.Printf("Failure rendering page: %s", err.Error())
		send500Error(rw, "Failed to render page")
		return
	}
	log.Print("Rendered list items page.")
}

// handlerUpdateReadFlags handles an update read flags request.
// it implements the type RequestHandlerFunc
// we update the requested flags in the database, and then redirect us
// back to the list of items page.
func handlerUpdateReadFlags(rw http.ResponseWriter, request *http.Request,
	settings *GorseConfig, session *sessions.Session) {
	// we should have some posted request values.
	// in order to get at these, we have to run ParseForm().
	err := request.ParseForm()
	if err != nil {
		log.Printf("Failed to parse form: %s", err.Error())
		send500Error(rw, "Failed to parse request")
		return
	}

	db, err := getDB(settings)
	if err != nil {
		log.Printf("Failed to get database connection: %s", err)
		send500Error(rw, "Failed to connect to database")
		return
	}

	// check if we have any items to mark as read. these are in
	// the request key 'read_item'.
	readItems, exists := request.PostForm["read_item"]
	setReadCount := 0
	if exists {
		// this is associated with a slice of strings. each of these
		// is an id we want to mark as read now.
		for _, idStr := range readItems {
			var id int64
			id, err = strconv.ParseInt(idStr, 10, 64)
			if err != nil {
				log.Printf("Failed to parse id into an integer %s: %s",
					idStr, err.Error())
				send500Error(rw, "Invalid id")
				return
			}

			err = setItemRead(db, id)
			if err != nil {
				send500Error(rw, "Unable to update read flag for "+idStr)
				return
			}

			setReadCount++
		}
	}

	log.Printf("Set %d item(s) read", setReadCount)

	session.AddFlash("Updated read flags")

	err = session.Save(request, rw)
	if err != nil {
		log.Printf("Unable to save session: %s", err)
		send500Error(rw, "Failed to save your session.")
		return
	}

	// TODO: should we get path from the config?
	var uri = "/gorse/?sort-order=" +
		url.QueryEscape(request.PostForm.Get("sort-order"))

	http.Redirect(rw, request, uri, http.StatusFound)
}

// handlerStaticFiles serves up some static files.
// it implements the type RequestHandlerFunc
// while it may be 'better' to serve these through a 'real' httpd, this
// simplifies setup, so support this method too.
func handlerStaticFiles(rw http.ResponseWriter, request *http.Request,
	settings *GorseConfig, session *sessions.Session) {
	log.Printf("Serving static request [%s]", request.URL.Path)

	// set the dir we serve.
	// TODO: possibly we should get this from a config and use an absolute
	//   path?
	var staticDir = http.Dir("./static")

	// create the fileserver handler that deals with the internals for us.
	var fileserverHandler = http.FileServer(staticDir)

	// we want to serve up the directory without the global uri prefix
	// since it is relative / may bare no resemblance to the request path.
	var strippedHandler = http.StripPrefix(settings.URIPrefix+"/static/",
		fileserverHandler)
	strippedHandler.ServeHTTP(rw, request)
}

// ServeHTTP handles an http request. it is invoked by the fastcgi
// package in a goroutine.
func (handler HTTPHandler) ServeHTTP(rw http.ResponseWriter,
	request *http.Request) {
	log.Printf("Serving new [%s] request from [%s] to path [%s]",
		request.Method, request.RemoteAddr, request.URL.Path)

	// Get existing session, or make a new one.
	session, err := handler.sessionStore.Get(request, handler.settings.SessionName)
	if err != nil {
		log.Printf("Session Get error: %s", err.Error())
		send500Error(rw, "Failed to get your session.")
		context.Clear(request)
		return
	}

	// We need to decide how to parse this request. we do this by looking
	// at the HTTP method and the path.

	type RequestHandlerFunc func(http.ResponseWriter, *http.Request,
		*GorseConfig, *sessions.Session)

	type RequestHandler struct {
		Method string

		// Regex pattern on the path to match.
		PathPattern string

		Func RequestHandlerFunc
	}

	var handlers = []RequestHandler{
		// GET /
		RequestHandler{
			Method:      "GET",
			PathPattern: "^" + handler.settings.URIPrefix + "/?$",
			Func:        handlerListItems,
		},

		// POST /update_read_flags
		RequestHandler{
			Method:      "POST",
			PathPattern: "^" + handler.settings.URIPrefix + "/update_read_flags/?$",
			Func:        handlerUpdateReadFlags,
		},

		// GET /static/*
		RequestHandler{
			Method:      "GET",
			PathPattern: "^" + handler.settings.URIPrefix + "/static/",
			Func:        handlerStaticFiles,
		},
	}

	// Find a matching handler.
	for _, actionHandler := range handlers {
		if actionHandler.Method != request.Method {
			continue
		}

		matched, err := regexp.MatchString(actionHandler.PathPattern,
			request.URL.Path)
		if err != nil {
			log.Printf("Error matching regex: %s", err.Error())
			continue
		}

		if matched {
			actionHandler.Func(rw, request, handler.settings, session)
			// NOTE: we don't session.Save() here as if we redirect the Save()
			//   won't take effect.
			// clean up gorilla globals. sessions package says this must be
			// done or we'll leak memory.
			context.Clear(request)
			return
		}
	}

	// There was no matching handler - send a 404.
	log.Printf("No handler for this request.")
	rw.WriteHeader(http.StatusNotFound)
	_, _ = rw.Write([]byte("<h1>404 Not Found</h1>"))
	_ = session.Save(request, rw)
	context.Clear(request)
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime)

	configPath := flag.String("config-file", "",
		"Path to a configuration file.")
	logPath := flag.String("log-file", "",
		"Path to a log file.")
	wwwPath := flag.String("www-path", "",
		"Path to directory containing assets: static and templates directories.")
	flag.Parse()

	if len(*configPath) == 0 {
		log.Print("You must specify a configuration file.")
		flag.PrintDefaults()
		os.Exit(1)
	}
	if len(*logPath) == 0 {
		log.Print("You must specify a log file.")
		flag.PrintDefaults()
		os.Exit(1)
	}
	if len(*wwwPath) == 0 {
		log.Print("You must specify a www path.")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// open log file.
	// don't use os.Create() because that truncates.
	logFh, err := os.OpenFile(*logPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
	if err != nil {
		log.Printf("Failed to open log file: %s: %s", *logPath, err.Error())
		os.Exit(1)
	}
	log.SetOutput(logFh)

	// chdir to the www path so we can get what we need to serve up.
	err = os.Chdir(*wwwPath)
	if err != nil {
		log.Printf("Unable to chdir to www directory: %s: %s", *wwwPath,
			err.Error())
		os.Exit(1)
	}

	// load up our settings.
	var settings GorseConfig
	err = config.GetConfig(*configPath, &settings)
	if err != nil {
		log.Printf("Failed to retrieve config: %s", err.Error())
		os.Exit(1)
	}

	// set up our session store.
	var sessionStore = sessions.NewCookieStore(
		[]byte(settings.CookieAuthenticationKey))

	// start listening.
	var listenHostPort = fmt.Sprintf("%s:%d", settings.ListenHost,
		settings.ListenPort)
	listener, err := net.Listen("tcp", listenHostPort)
	if err != nil {
		log.Print("Failed to open port: " + err.Error())
		os.Exit(1)
	}

	var httpHandler = HTTPHandler{
		settings:     &settings,
		sessionStore: sessionStore,
	}

	// TODO: this will serve requests forever - should we have a signal
	//   or a method to cause this to gracefully stop?
	log.Print("Starting to serve requests.")
	err = fcgi.Serve(listener, httpHandler)
	if err != nil {
		log.Print("Failed to start serving HTTP: " + err.Error())
		os.Exit(1)
	}
}
