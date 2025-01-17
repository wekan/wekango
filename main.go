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
    return "5000"
}

func main() {
    mongoURL := getMongoURL()
    var status string

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

    // Create HTTP handler
    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintf(w, "<pre>%s</pre>", status)
    })

    // Start web server
    port := getWebPort()
    log.Printf("Server starting on port %s", port)
    if err := http.ListenAndServe(":"+port, nil); err != nil {
        log.Fatal(err)
    }
}
