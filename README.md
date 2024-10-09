
# Benchmarking Assignment

This tool benchmarks SELECT query performance across multiple workers, retrieving max and min CPU usage per minute for a given hostname within a specified time range from a TimescaleDB instance.

## Design Doc
- https://docs.google.com/document/d/1K0qkjui4aPh1GPOms9nT3yP5b94w9MMVYSCPlHHeXtA/edit?usp=sharing 

## Prerequisites

- Docker and Docker Compose installed
- Go 1.21+ (if running locally)

## How to Run with Docker

1. Clone the repository:

   ```bash
   git clone https://github.com/benbz1/Benchmarking.git
   cd Benchmarking
   ```

2. Set up TimescaleDB and create the required tables:

   ```bash
   docker-compose up timescaledb
   ```

3. Load the data into the `cpu_usage` table:

   ```bash
   docker exec -it timescaledb psql -U postgres -d homework -c "\COPY cpu_usage FROM '/app/cpu_usage.csv' CSV HEADER"
   ```

4. Run the application: To benchmark the queries with a specified number of workers:

   ```bash
   WORKERS=3 docker-compose up app
   ```

5. Stop the services:

   ```bash
   docker-compose down
   ```

## How to Run Locally (Without Docker)

1. Install all dependencies using:

   ```bash
   go mod tidy
   ```

2. Ensure TimescaleDB is running locally and the `cpu_usage` table is populated.

3. Comment out lines (37-41) and replace `connStr` with your connection string.

4. Run the application using Go:

   Using a file:
   ```bash
   go run main.go -file=data/query_params.csv -workers=3
   ```

   Or using STDIN:
   ```bash
   cat data/query_params.csv | go run main.go -workers=3
   ```

## Configuration

- **Data**: You can replace the table data in [`./data/cpu_usage.csv`](./data/cpu_usage.csv).

- **Number of Workers**: You can adjust the number of workers by setting the `WORKERS` environment variable when running the application via Docker.

- **Queries**: You can replace the input with different queries in [`./data/query_params.csv`](./data/query_params.csv).


## Comments & Next Steps
