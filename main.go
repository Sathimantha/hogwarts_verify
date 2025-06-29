package main

import (
	"database/sql"
	"fmt"
	"html"
	"log"
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
		log.Fatalf("Error loading .env file: %v", err)
	}

	dbUser := os.Getenv("DB_USERNAME")
	dbPass := os.Getenv("DB_PASSWORD")
	dbHost := os.Getenv("DB_HOST")
	dbPort := os.Getenv("DB_PORT")
	dbName := os.Getenv("DB_NAME")

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s", dbUser, dbPass, dbHost, dbPort, dbName)
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
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
	// Twilio's /twilio/verify doesn't need CORS, but it won't be affected
	http.Handle("/", corsHandler(r))

	certFile := os.Getenv("CERT_FILE")
	keyFile := os.Getenv("KEY_FILE")
	if certFile == "" || keyFile == "" {
		log.Fatalf("CERT_FILE or KEY_FILE not defined in .env")
	}

	log.Println("Server started on :5001 with SSL")
	err = http.ListenAndServeTLS(":5001", certFile, keyFile, nil)
	if err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func twilioVerifyHandler(w http.ResponseWriter, r *http.Request) {
	// Extract input from Twilio (Digits for DTMF, SpeechResult for speech)
	input := r.PostFormValue("Digits")
	if input == "" {
		input = r.PostFormValue("SpeechResult")
	}
	if input == "" {
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
		w.Write([]byte(twiml))
		return
	}

	var fullName, category string
	// Use LIKE to match input with or without trailing 'v'
	query := `SELECT full_name, category FROM people WHERE national_id LIKE ? LIMIT 1`
	err := db.QueryRow(query, input+"%").Scan(&fullName, &category)

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
	<Say>You entered %s. The name is %s, and it is verified to be a %s member.</Say>
	<Say>Thank You For Contacting Hogwarts.</Say>
	<Hangup/>
</Response>`, input, fullName, categoryText)
		w.Write([]byte(twiml))
	} else {
		// Generate TwiML for no match, including entered input
		twiml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Response>
	<Say>Sorry, no match found for %s.</Say>
	<Say>Thank You For Contacting Hogwarts.</Say>
	<Hangup/>
</Response>`, input)
		w.Write([]byte(twiml))
	}
}

func isDigits(s string) bool {
	return digitRegex.MatchString(s)
}

func verifyHandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "ID is required", http.StatusBadRequest)
		return
	}

	// Validate ID format (alphanumeric, max 50 chars)
	if len(id) > 50 || !regexp.MustCompile(`^[a-zA-Z0-9]+$`).MatchString(id) {
		http.Error(w, "Invalid ID format", http.StatusBadRequest)
		return
	}

	var fullName, category, remark string
	query := `SELECT full_name, category, remark FROM people WHERE national_id = ? LIMIT 1`
	err := db.QueryRow(query, id).Scan(&fullName, &category, &remark)
	if err == sql.ErrNoRows {
		http.Error(w, "Person not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		log.Printf("Database error: %v", err)
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

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(htmlResponse))
}
