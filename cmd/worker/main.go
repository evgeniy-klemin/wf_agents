package main

import (
	"log"

	"github.com/eklemin/wf-agents/internal/config"
	"github.com/eklemin/wf-agents/internal/noplog"
	wf "github.com/eklemin/wf-agents/internal/workflow"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

const taskQueue = "coding-session"

func main() {
	// Load and validate workflow config at startup.
	cfg, err := config.DefaultConfig()
	if err != nil {
		log.Fatalf("Failed to load workflow config: %v", err)
	}
	if errs := config.ValidateWorkflowConfig(cfg); len(errs) > 0 {
		for _, e := range errs {
			log.Printf("WARNING: workflow config: %v", e)
		}
	}

	c, err := client.Dial(client.Options{
		HostPort: "localhost:7233",
		Logger:   noplog.New(),
	})
	if err != nil {
		log.Fatalf("Failed to connect to Temporal: %v", err)
	}
	defer c.Close()

	w := worker.New(c, taskQueue, worker.Options{})
	w.RegisterWorkflow(wf.CodingSessionWorkflow)

	log.Printf("Starting worker on task queue %q", taskQueue)
	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatalf("Worker failed: %v", err)
	}
}
