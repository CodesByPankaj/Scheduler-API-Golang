package main

import (
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

type Job struct {
	ID       int           `json:"id"`
	Name     string        `json:"name"`
	Duration time.Duration `json:"duration"`
	Status   string        `json:"status"`
}

//SJF scheduling
type JobScheduler struct {
	jobs     []*Job
	jobMutex sync.Mutex
	clients  map[*websocket.Conn]bool
	upgrader websocket.Upgrader
}


func NewJobScheduler() *JobScheduler {
	return &JobScheduler{
		clients: make(map[*websocket.Conn]bool),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				if origin == "http://localhost:3000" {
					return true
				}
				return false
			},
		},
	}
}
func (scheduler *JobScheduler) scheduleJobs() {
	scheduler.jobMutex.Lock()
	defer scheduler.jobMutex.Unlock()

	sort.Slice(scheduler.jobs, func(i, j int) bool {
		return scheduler.jobs[i].Duration < scheduler.jobs[j].Duration
	})
}

//new job
func (scheduler *JobScheduler) addJob(name string, duration time.Duration) *Job {
	scheduler.jobMutex.Lock()
	defer scheduler.jobMutex.Unlock()

	job := &Job{
		ID:       rand.Intn(1000),
		Name:     name,
		Duration: duration,
		Status:   "pending",
	}
	scheduler.jobs = append(scheduler.jobs, job)
	scheduler.scheduleJobs()
	scheduler.broadcastJobStatus()
	return job
}

func (scheduler *JobScheduler) broadcastJobStatus() {
	scheduler.jobMutex.Lock()
	defer scheduler.jobMutex.Unlock()

	jobsJSON, err := json.Marshal(scheduler.jobs)
	if err != nil {
		log.Println("Error marshalling jobs:", err)
		return
	}

	for client := range scheduler.clients {
		err := client.WriteMessage(websocket.TextMessage, jobsJSON)
		if err != nil {
			log.Printf("Error sending message to client: %v", err)
			client.Close()
			delete(scheduler.clients, client)
		}
	}
}
func (scheduler *JobScheduler) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	ws, err := scheduler.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Error upgrading to WebSocket:", err)
		return
	}
	scheduler.clients[ws] = true
	scheduler.broadcastJobStatus()
}

func (scheduler *JobScheduler) runJobs() {
	for {
		scheduler.jobMutex.Lock()
		if len(scheduler.jobs) == 0 {
			scheduler.jobMutex.Unlock()
			time.Sleep(1 * time.Second)
			continue
		}
		job := scheduler.jobs[0]
		job.Status = "running"
		scheduler.broadcastJobStatus()
		scheduler.jobMutex.Unlock()

		time.Sleep(job.Duration)
		scheduler.jobMutex.Lock()
		job.Status = "completed"
		scheduler.jobs = scheduler.jobs[1:]
		scheduler.broadcastJobStatus()
		scheduler.jobMutex.Unlock()
	}
}
func (scheduler *JobScheduler) submitJobHandler(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name     string `json:"name"`
		Duration int    `json:"duration"`
	}
	err := json.NewDecoder(r.Body).Decode(&request)
	if err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	job := scheduler.addJob(request.Name, time.Duration(request.Duration)*time.Second)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(job)
}

func (scheduler *JobScheduler) getJobsHandler(w http.ResponseWriter, r *http.Request) {
	scheduler.jobMutex.Lock()
	defer scheduler.jobMutex.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(scheduler.jobs)
}

func main() {
	scheduler := NewJobScheduler()
	go scheduler.runJobs()

	router := mux.NewRouter()
	router.HandleFunc("/jobs", scheduler.submitJobHandler).Methods("POST")
	router.HandleFunc("/jobs", scheduler.getJobsHandler).Methods("GET")
	router.HandleFunc("/ws", scheduler.handleWebSocket)
	corsOptions := handlers.CORS(
		handlers.AllowedOrigins([]string{"http://localhost:3000"}),
		handlers.AllowedMethods([]string{"GET", "POST", "OPTIONS"}),
		handlers.AllowedHeaders([]string{"Content-Type", "Authorization"}),
	)
	log.Println("Server started on :8080")
	log.Fatal(http.ListenAndServe(":8080", corsOptions(router)))
}