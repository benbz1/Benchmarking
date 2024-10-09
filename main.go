package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"math"

	"github.com/jackc/pgx/v5"
)

// Query structure
type Query struct {
	hostname  string
	startTime string
	endTime   string
}

// Worker structure
type Worker struct {
	id      int
	queries chan Query
	wg      *sync.WaitGroup
	times   []time.Duration
	db      *pgx.Conn
}

// Retry connecting to the database up to 3 times
func connectToDatabase() (*pgx.Conn, error) {
	dbHost := os.Getenv("DB_HOST")
	dbPort := os.Getenv("DB_PORT")
	dbUser := os.Getenv("DB_USER")
	dbPassword := os.Getenv("DB_PASSWORD")
	dbName := os.Getenv("DB_NAME")

	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", dbUser, dbPassword, dbHost, dbPort, dbName)

	// TODO (Ben - 10/08/2024): Improve error handling for database connection failures
	var dbConn *pgx.Conn
	var err error
	// TODO (Ben - 10/08/2024): Implement database connection pooling to handle more queries efficiently.
	for i := 0; i < 3; i++ {
		dbConn, err = pgx.Connect(context.Background(), connStr)
		if err == nil {
			return dbConn, nil
		}
		log.Printf("Failed to connect to database, attempt %d: %v", i+1, err)
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("failed to connect to database after 3 attempts: %v", err)
}

// Worker processes queries from its queue
func (w *Worker) start() {
	for query := range w.queries {
		start := time.Now()
		fmt.Printf("Worker %d processing query for hostname %s from %s to %s\n", w.id, query.hostname, query.startTime, query.endTime)

		// Generate SQL query to get max and min CPU usage per minute
		sqlQuery := fmt.Sprintf(`
			SELECT 
				time_bucket('1 minute', ts) AS minute, 
				MAX(usage) AS max_usage, 
				MIN(usage) AS min_usage 
			FROM cpu_usage 
			WHERE host = '%s' 
			  AND ts BETWEEN '%s' AND '%s' 
			GROUP BY minute 
			ORDER BY minute;
		`, query.hostname, query.startTime, query.endTime)

		// Execute SQL query
		// TODO (Ben - 10/08/2024): Add query timeout mechanism to prevent long-running queries. 
		// TODO (Ben - 10/08/2024): Consider implementing caching to avoid re-processing the same queries.
		rows, err := w.db.Query(context.Background(), sqlQuery)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Worker %d failed to execute query: %v\n", w.id, err)
			w.wg.Done()
			continue
		}
		defer rows.Close()

		for rows.Next() {
			var minute time.Time
			var maxUsage, minUsage float64
			err = rows.Scan(&minute, &maxUsage, &minUsage)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error scanning row: %v\n", err)
				continue
			}
			fmt.Printf("Hostname: %s, Minute: %s, Max Usage: %f, Min Usage: %f\n", query.hostname, minute, maxUsage, minUsage)
		}

		duration := time.Since(start)
		// TODO (Ben - 10/08/2024): Consider maintaining a more advanced data structure to track query results for deeper analysis by host, date, etc.
		w.times = append(w.times, duration)
		w.wg.Done()
	}
}

// Assign worker based on the number of queries in their queue
func assignWorker(hostname string, workerMap map[string]*Worker, workers []*Worker) *Worker {
	if worker, exists := workerMap[hostname]; exists {
		return worker
	}

	// Iterate through workers to find the one with the least number of queries
	var selectedWorker *Worker
	minQueries := math.MaxInt

	for _, worker := range workers {
		if len(worker.queries) < minQueries {
			minQueries = len(worker.queries)
			selectedWorker = worker
		}
	}

	// Assign the worker to this hostname for future queries
	workerMap[hostname] = selectedWorker

	return selectedWorker
}

// Calculate median time from slice of durations
func calculateMedian(times []time.Duration) time.Duration {
	if len(times) == 0 {
		return 0
	}
	sort.Slice(times, func(i, j int) bool {
		return times[i] < times[j]
	})
	n := len(times)
	if n%2 == 1 {
		return times[n/2]
	}
	return (times[n/2-1] + times[n/2]) / 2
}

func main() {
	// Parse flags for input file and number of workers
	var inputFile string
	var numWorkers int
	flag.StringVar(&inputFile, "file", "", "Path to the input CSV file. Leave blank to use STDIN.")
	// TODO (Ben - 10/08/2024): Consider setting a maximum number of workers, passed in by user input, to control concurrency and optimize resource usage.
	flag.IntVar(&numWorkers, "workers", 3, "Number of concurrent workers.")
	flag.Parse()

	var reader *csv.Reader
	if inputFile != "" {
		// TODO (Ben - 10/08/2024): Add better validation for CSV input data
		file, err := os.Open(inputFile)
		if err != nil {
			log.Fatalf("failed to open file: %v", err)
		}
		defer file.Close()
		reader = csv.NewReader(file)
	} else {
		// STDIN
		// TODO (Ben - 10/08/2024): Add better validation for STDIN input data
		reader = csv.NewReader(os.Stdin)
	}

	// Connect to the TimescaleDB instance
	var wg sync.WaitGroup
	workers := make([]*Worker, numWorkers)
	workerMap := make(map[string]*Worker)

	for i := 0; i < numWorkers; i++ {
		dbConn, err := connectToDatabase()
		if err != nil {
			log.Fatalf("Worker %d: Failed to connect to database: %v", i, err)
		}
		worker := &Worker{
			id:      i,
			queries: make(chan Query, 10), // TODO (Ben - 10/08/2024): Revisit the channel buffer size. Consider removing/adjusting based on query load and worker processing time to avoid unnecessary blocking.
			wg:      &wg,
			times:   make([]time.Duration, 0),
			db:      dbConn,
		}
		workers[i] = worker
		go worker.start()
	}

	// Read and assign queries line by line
	for {
		record, err := reader.Read()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			log.Printf("Skipping malformed record: %v", err)
			continue
		}

		// Handle malformed or incomplete records
		if len(record) < 3 || strings.TrimSpace(record[0]) == "" || strings.TrimSpace(record[1]) == "" || strings.TrimSpace(record[2]) == "" {
			log.Printf("Skipping invalid record: %v", record)
			continue
		}

		hostname := record[0]
		startTime := record[1]
		endTime := record[2]
		query := Query{hostname: hostname, startTime: startTime, endTime: endTime}

		wg.Add(1)
		worker := assignWorker(hostname, workerMap, workers)
		worker.queries <- query
	}

	wg.Wait()

	for _, worker := range workers {
		close(worker.queries)
		worker.db.Close(context.Background())
	}

	// Benchmark stats
	totalQueries := 0
	totalTime := time.Duration(0)
	minTime := time.Duration(math.MaxInt64)
	maxTime := time.Duration(0)
	var allDurations []time.Duration

	for _, worker := range workers {
		for _, duration := range worker.times {
			totalQueries++
			totalTime += duration
			if duration < minTime {
				minTime = duration
			}
			if duration > maxTime {
				maxTime = duration
			}
			allDurations = append(allDurations, duration)
		}
	}

	avgTime := totalTime / time.Duration(totalQueries)
	medianTime := calculateMedian(allDurations)

	fmt.Printf("Total queries: %d\n", totalQueries)
	fmt.Printf("Total processing time: %v\n", totalTime)
	fmt.Printf("Min query time: %v\n", minTime)
	fmt.Printf("Avg query time: %v\n", avgTime)
	fmt.Printf("Max query time: %v\n", maxTime)
	fmt.Printf("Median query time: %v\n", medianTime)
}
