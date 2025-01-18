# Testing local webserver with DDoS-level traffic, does webserver handle it and work OK, or crash.
# Do not use this to test any Internet server, it is illegal, police may contact you.

wrk -t12 -c100 -d30s http://127.0.0.1:8000/
#wrk -t12 -c100 -d300s http://127.0.0.1:8000/
#wrk -t12 -c100 -d30s http://127.0.0.1:7000/sign-in
