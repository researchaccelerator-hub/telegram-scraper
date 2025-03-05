package dapr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	daprc "github.com/dapr/go-sdk/client"
	"github.com/dapr/go-sdk/service/common"
	daprs "github.com/dapr/go-sdk/service/grpc"
	common2 "github.com/researchaccelerator-hub/telegram-scraper/common"
	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/types/known/anypb"
)

// StartDaprMode initializes and starts a Dapr service in job mode using the provided
// crawler configuration. It sets up service invocation handlers for scheduling and
// retrieving jobs, and registers job event handlers for predefined job names. The
// service listens for Dapr requests on the specified port and logs relevant information
// and errors during the process.
func StartDaprMode(crawlerCfg common2.CrawlerConfig) {
	log.Info().Msg("Starting crawler in DAPR job mode")
	log.Printf("Listening on port %d for DAPR requests", crawlerCfg.DaprPort)

	//Create new Dapr client
	daprClient, err := daprc.NewClient()
	if err != nil {
		panic(err)
	}
	defer daprClient.Close()

	app = App{
		daprClient: daprClient,
	}

	// Create a new Dapr service
	port := fmt.Sprintf(":%d", crawlerCfg.DaprPort)
	server, err := daprs.NewService(port)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to start server: %v")
	}

	// Creates handlers for the service
	if err := server.AddServiceInvocationHandler("scheduleJob", scheduleJob); err != nil {
		log.Fatal().Err(err).Msg("error adding invocation handler")
	}

	if err := server.AddServiceInvocationHandler("getJob", getJob); err != nil {
		log.Fatal().Err(err).Msg("error adding invocation handler")
	}

	//if err := server.AddServiceInvocationHandler("deleteJob", deleteJob); err != nil {
	//	log.Fatal().Err(err).Msg("error adding invocation handler: %v", err)
	//}

	// Register job event handler for all jobs
	for _, jobName := range jobNames {
		if err := server.AddJobEventHandler(jobName, handleJob); err != nil {
			log.Fatal().Err(err).Msg("failed to register job event handler")
		}
		log.Info().Msgf("Registered job handler for: %s", jobName)
	}

	log.Info().Msgf("Starting server on port: %s", port)
	if err = server.Start(); err != nil {
		log.Fatal().Err(err).Msg("failed to start server")
	}
}

type App struct {
	daprClient daprc.Client
}

var app App

var jobNames = []string{"R2-D2", "C-3PO", "BB-8", "my-scheduled-job"}

type DroidJob struct {
	Name    string `json:"name"`
	Job     string `json:"job"`
	DueTime string `json:"dueTime"`
}

// scheduleJob handles the scheduling of a job based on the provided invocation event.
// It unmarshals the event data into a DroidJob structure, constructs a JobData object,
// and marshals it into JSON format. The job is then scheduled using the Dapr client.
// Returns the original invocation event content and any error encountered during the process.
//
// Parameters:
// - ctx: The context for the operation.
// - in: The invocation event containing job details.
//
// Returns:
// - out: The content of the invocation event.
// - err: An error if the job scheduling fails.
func scheduleJob(ctx context.Context, in *common.InvocationEvent) (out *common.Content, err error) {

	if in == nil {
		err = errors.New("no invocation parameter")
		return
	}

	droidJob := DroidJob{}
	err = json.Unmarshal(in.Data, &droidJob)
	if err != nil {
		log.Error().Err(err).Msgf("failed to unmarshal job: %v", err)
		return nil, err
	}

	jobData := JobData{
		Droid: droidJob.Name,
		Task:  droidJob.Job,
	}

	content, err := json.Marshal(jobData)
	if err != nil {
		log.Error().Err(err).Msg("Error marshalling job content")
		return nil, err
	}

	// schedule job
	job := daprc.Job{
		Name:    droidJob.Name,
		DueTime: droidJob.DueTime,
		Data: &anypb.Any{
			Value: content,
		},
	}

	err = app.daprClient.ScheduleJobAlpha1(ctx, &job)
	if err != nil {
		log.Error().Msgf("failed to schedule job. err: %v", err)
		return nil, err
	}

	log.Info().Msgf("Job scheduled: %v", droidJob.Name)

	out = &common.Content{
		Data:        in.Data,
		ContentType: in.ContentType,
		DataTypeURL: in.DataTypeURL,
	}

	return out, err

}

// getJob retrieves a job by its name using the provided invocation event.
// It fetches the job data from the Dapr client and returns it in a common.Content structure.
//
// Parameters:
// - ctx: The context for the operation.
// - in: The invocation event containing the job name.
//
// Returns:
// - out: The content of the job retrieved.
// - err: An error if the job retrieval fails.
func getJob(ctx context.Context, in *common.InvocationEvent) (out *common.Content, err error) {

	if in == nil {
		err = errors.New("no invocation parameter")
		return nil, err
	}

	job, err := app.daprClient.GetJobAlpha1(ctx, string(in.Data))
	if err != nil {
		log.Error().Err(err).Msgf("failed to get job. err: %v", err)
	}

	out = &common.Content{
		Data:        job.Data.Value,
		ContentType: in.ContentType,
		DataTypeURL: in.DataTypeURL,
	}

	return out, err
}

type JobData struct {
	Droid string `json:"droid"`
	Task  string `json:"Task"`
}

// handleJob processes a job event by unmarshaling the job data and payload,
// then logs the droid and task information. It returns an error if unmarshaling fails.
//
// Parameters:
// - ctx: The context for the operation.
// - job: The job event containing the job data.
//
// Returns:
// - error: An error if the job data or payload unmarshaling fails.
func handleJob(ctx context.Context, job *common.JobEvent) error {
	log.Info().Msgf("Job event received! Raw data: %s", string(job.Data))
	log.Info().Msgf("Job type: %s", job.JobType)
	var jobData common.Job
	if err := json.Unmarshal(job.Data, &jobData); err != nil {
		return fmt.Errorf("failed to unmarshal job: %v", err)
	}

	var jobPayload JobData
	if err := json.Unmarshal(job.Data, &jobPayload); err != nil {
		return fmt.Errorf("failed to unmarshal payload: %v", err)
	}

	log.Info().Msgf("Starting droid: %s", jobPayload.Droid)
	log.Info().Msgf("Executing maintenance job: %s", jobPayload.Task)

	return nil
}
