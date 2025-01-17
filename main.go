package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"wekango/mongo3"
	"wekango/mongo6"
)

func getMongoPort() string {
	if port := os.Getenv("MONGODB_PORT"); port != "" {
		return port
	}
	return "27017"
}

func getMongoURL() string {
	if url := os.Getenv("MONGO_URL"); url != "" {
		return url
	}
	return fmt.Sprintf("mongodb://localhost:%s", getMongoPort())
}

func getWebPort() string {
	if port := os.Getenv("PORT"); port != "" {
		return port
	}
	return "8000"
}

func main() {
	mongoURL := getMongoURL()
	var status string
	var page string

	// Connect using MongoDB 3 driver
	client3, err := mongo3.Connect(mongoURL)
	if err != nil {
		status += fmt.Sprintf("Failed to connect using MongoDB 3 driver: %v\n", err)
	} else {
		defer client3.Close()
		status += "Connected using MongoDB 3 driver\n"
	}

	// Connect using MongoDB 6 driver
	client6, err := mongo6.Connect(mongoURL)
	if err != nil {
		status += fmt.Sprintf("Failed to connect using MongoDB 6 driver: %v\n", err)
	} else {
		defer func() {
			if err := client6.Disconnect(context.Background()); err != nil {
				log.Printf("Error disconnecting MongoDB 6 client: %v", err)
			}
		}()
		status += "Connected using MongoDB 6 driver\n"
	}

	page += fmt.Sprintf(`

<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 4.01//EN"
        "http://www.w3.org/TR/html4/strict.dtd">
<html>
<head>
  <title>WeKan</title>
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta http-equiv="X-UA-Compatible" content="IE=edge">
  <link rel="shortcut icon" type="image/x-icon" href="images/favicon.ico">
  <link rel="apple-touch-icon" sizes="180x180" href="images/apple-touch-icon.png">
  <link rel="icon" type="image/png" sizes="32x32" href="images/favicon-32x32.png">
  <link rel="icon" type="image/png" sizes="16x16" href="images/favicon-16x16.png">
  <link rel="manifest" crossOrigin="use-credentials" href="site.webmanifest">
  <link rel="mask-icon" href="images/safari-pinned-tab.svg" color="#5bbad5">
  <meta name="apple-mobile-web-app-title" content="WeKan">
  <meta name="application-name" content="WeKan">
  <meta name="msapplication-TileColor" content="#00aba9">
  <meta name="theme-color" content="#ffffff">

    <style>
        .kanban {
            display: flex;
            justify-content: space-around;
            width: 80%;
            margin: 0 auto;
        }
        .column {
            border: 1px solid #ddd;
            padding: 10px;
            margin: 10px;
            width: 25%;
        }
        .column h3 {
            text-align: center;
        }
        .card {
            padding: 10px;
            margin-bottom: 10px;
            border-radius: 5px;
            background-color: #eee;
            cursor: grab;
        }
        .card:hover {
            background-color: #ddd;
        }
    </style>
</head>

<body>




<h2>WeKan</h2>

<p><a href="/">All Pages</a></p>

<p><b>Login</b>: <a href="/sign-in">Sign In</a> - <a href="/sign-up">Sign Up</a>
 - <a href="/forgot-password">Forgot Password</a></p>

<p><b>Pet</b>: <a href="/owners/new">New Owner</a>
 - <a href="/owners/find">Find Owners</a></p>

<p><b>Boards</b>: <a href="/allboards">All Boards</a>
 - <a href="/allboards2">All Boards 2</a> - <a href="/allboards3">All Boards 3</a> -

<a href="/public">Public</a> - <a href="/board">Board</a>
 - <a href="/shortcuts">Shortcuts</a> - <a href="/templates">Templates</a></p>

<p><b>Cards</b>: <a href="/my-cards">My Cards</a>
 - <a href="/due-cards">Due Cards</a> - <a href="/import">Import</a></p>

<p><b>Search</b>: <a href="/global-search">Global Search</a>

<p><b>Admin</b>: <a href="/broken-cards">Broken Cards</a> - <a href="/setting">Setting</a>
 - <a href="/information">Information</a> - <a href="/people">People</a>
 - <a href="/admin-reports">Admin Reports</a> - <a href="/attachments">Attachments</a>
 - <a href="/translation">Translation</a></p>

<p><b>Error</b>: <a href="/oops">Show Error</a></p>



    <script>
        const cards = document.querySelectorAll('.card');
        const columns = document.querySelectorAll('.column');

        cards.forEach(card => {
            card.addEventListener('dragstart', function() {
                this.classList.add('dragging');
            });

            card.addEventListener('dragend', function() {
                this.classList.remove('dragging');
            });
        });

        columns.forEach(column => {
            column.addEventListener('dragover', function(event) {
                if (event.target.classList.contains('card')) {
                    return;
                }
                event.preventDefault();
                const draggingCard = document.querySelector('.dragging');
                this.appendChild(draggingCard);
            });
        });
    </script>

</body>

</html>`, err)

	// Create HTTP handler
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		//fmt.Fprintf(w, "<pre>%s</pre>", status)
		fmt.Fprintf(w, "%s", page)
	})

	// Start web server
	port := getWebPort()
	log.Printf("Server starting on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}
