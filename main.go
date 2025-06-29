package main

import (
	"database/sql"
	"fmt"
	"html"
	"net/http"
	"os"
	"regexp"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
)

var db *sql.DB
var digitRegex = regexp.MustCompile(`^\d+$`)

func main() {
	err := godotenv.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading .env file: %v\n", err)
		os.Exit(1)
	}

	dbUser := os.Getenv("DB_USERNAME")
	dbPass := os.Getenv("DB_PASSWORD")
	dbHost := os.Getenv("DB_HOST")
	dbPort := os.Getenv("DB_PORT")
	dbName := os.Getenv("DB_NAME")

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s", dbUser, dbPass, dbHost, dbPort, dbName)
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to DB: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	r := mux.NewRouter()

	// Define routes
	r.HandleFunc("/verify", verifyHandler).Methods("GET")
	r.HandleFunc("/twilio/verify", twilioVerifyHandler).Methods("POST")

	// Apply CORS only to /verify for frontend
	corsHandler := handlers.CORS(
		handlers.AllowedOrigins([]string{"https://hogwarts-legacy.info"}),
		handlers.AllowedMethods([]string{"GET", "POST"}),
		handlers.AllowedHeaders([]string{"Content-Type", "Accept"}),
	)

	// Wrap the entire router with CORS handler
	http.Handle("/", corsHandler(r))

	certFile := os.Getenv("CERT_FILE")
	keyFile := os.Getenv("KEY_FILE")
	if certFile == "" || keyFile == "" {
		fmt.Fprintf(os.Stderr, "CERT_FILE or KEY_FILE not defined in .env\n")
		os.Exit(1)
	}

	fmt.Println("Server started on :5001 with SSL")
	err = http.ListenAndServeTLS(":5001", certFile, keyFile, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Server failed: %v\n", err)
		os.Exit(1)
	}
}

func twilioVerifyHandler(w http.ResponseWriter, r *http.Request) {
	// Print raw query string to terminal
	fmt.Println("Raw query:", r.URL)

	// Extract input from query parameters (case-insensitive)
	query := r.URL.Query()
	input := query.Get("Digits")
	if input == "" {
		input = query.Get("digits")
	}
	if input == "" {
		input = query.Get("SpeechResult")
	}
	if input == "" {
		input = query.Get("speechresult")
	}
	// Print received parameters to terminal
	fmt.Printf("Received Digits: %s, SpeechResult: %s\n", query.Get("Digits"), query.Get("SpeechResult"))

	if input == "" {
		fmt.Println("No input provided, returning 400")
		http.Error(w, "No input provided", http.StatusBadRequest)
		return
	}

	// Remove spaces and normalize input
	input = strings.ReplaceAll(input, " ", "")

	// Validate input (alphanumeric, max 50 chars)
	if len(input) > 50 || !regexp.MustCompile(`^[a-zA-Z0-9]+$`).MatchString(input) {
		w.Header().Set("Content-Type", "application/xml")
		twiml := `<?xml version="1.0" encoding="UTF-8"?>
<Response>
	<Say>Invalid input format. Please use only numbers or letters.</Say>
	<Say>Thank You For Contacting Hogwarts.</Say>
	<Hangup/>
</Response>`
		fmt.Println("Invalid input format, returning TwiML")
		w.Write([]byte(twiml))
		return
	}

	var fullName, category string
	// Use LIKE to match input with or without trailing 'v'
	queryStr := `SELECT full_name, category FROM people WHERE national_id LIKE ? LIMIT 1`
	err := db.QueryRow(queryStr, input+"%").Scan(&fullName, &category)
	if err != nil {
		fmt.Printf("Database error for input %s: %v\n", input, err)
	}

	w.Header().Set("Content-Type", "application/xml")
	if err == nil {
		// Adjust category text for natural speech
		categoryText := "student"
		if category == "staff" {
			categoryText = "staff member"
		}
		// Generate TwiML for successful verification
		twiml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Response>
	<Say>You entered %s. The name is %s, and it is verified to be a %s.</Say>
	<Say>Thank You For Contacting Hogwarts.</Say>
	<Hangup/>
</Response>`, input, fullName, categoryText)
		fmt.Println("Successful verification, returning TwiML")
		w.Write([]byte(twiml))
	} else {
		// Generate TwiML for no match, including entered input
		twiml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Response>
	<Say>Sorry, no match found for %s.</Say>
	<Say>Thank You For Contacting Hogwarts.</Say>
	<Hangup/>
</Response>`, input)
		fmt.Println("No match found, returning TwiML")
		w.Write([]byte(twiml))
	}
}

func isDigits(s string) bool {
	return digitRegex.MatchString(s)
}

func verifyHandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		fmt.Println("No ID provided for /verify, returning 400")
		http.Error(w, "ID is required", http.StatusBadRequest)
		return
	}

	// Validate ID format (alphanumeric, max 50 chars)
	if len(id) > 50 || !regexp.MustCompile(`^[a-zA-Z0-9]+$`).MatchString(id) {
		fmt.Println("Invalid ID format for /verify, returning 400")
		http.Error(w, "Invalid ID format", http.StatusBadRequest)
		return
	}

	var fullName, category, remark string
	query := `SELECT full_name, category, remark FROM people WHERE national_id = ? LIMIT 1`
	err := db.QueryRow(query, id).Scan(&fullName, &category, &remark)
	if err == sql.ErrNoRows {
		fmt.Println("Person not found for /verify, returning 404")
		http.Error(w, "Person not found", http.StatusNotFound)
		return
	} else if err != nil {
		fmt.Printf("Database error for /verify ID %s: %v\n", id, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Escape values to prevent XSS
	safeID := html.EscapeString(id)
	safeName := html.EscapeString(fullName)

	var htmlResponse string
	if category == "student" {
		htmlResponse = fmt.Sprintf(`<div style="font-family: Arial, sans-serif; line-height: 1.6; padding: 10px;">
			<strong>ID:</strong> %s<br>
			<strong>FULL NAME:</strong> %s<br>
			<strong>COURSES COMPLETED:</strong><br>
			<ul>
				<li>Introduction to Basic Psychology (One Hour Workshop)</li>
				<li>Introduction to Career Guidance (One Hour Workshop)</li>
				<li>Introduction to Basic Counselling (One Hour Workshop)</li>
				<li>Introduction to Basic IT (One Hour Workshop)</li>
				<li>Introduction to Basic Business Management (One Hour Workshop)</li>
				<li>Introduction to Basic Spoken English (One Hour Workshop)</li>
				<li>Introduction to Memory Boosting (One Hour Workshop)</li>
				<li>Introduction to Basic Personality Development (One Hour Workshop)</li>
				<li>Introduction to Entrepreneurship (One Hour Workshop)</li>
				<li>Introduction to Basic Body Language (One Hour Workshop)</li>
				<li>Introduction to Basic Counselling Skills (One Hour Workshop)</li>
				<li>Introduction to Basic Human Resource Management (One Hour Workshop)</li>
				<li>Introduction to Basic Teaching Methodologies (One Hour Workshop)</li>
				<li>Introduction to Basic Marketing Management (One Hour Workshop)</li>
			</ul>
			<strong>APPROVED AND VERIFIED:</strong> YES
		</div>`, safeID, safeName)
	} else {
		htmlResponse = fmt.Sprintf(`<div style="font-family: Arial, sans-serif; line-height: 1.6; padding: 10px;">
			<strong>ID:</strong> %s<br>
			<strong>FULL NAME:</strong> %s<br>
			<strong>REMARKS:</strong><br>
			%s
		</div>`, safeID, safeName, remark)
	}

	fmt.Println("Returning HTML response for /verify")
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(htmlResponse))
}
