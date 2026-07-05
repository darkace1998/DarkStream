# Getting Started with DarkStream

This guide will walk you through setting up a basic DarkStream distributed video conversion cluster from scratch. We will build the binaries, configure the Master coordinator, start a Worker node, and use the CLI and Web Dashboard to monitor progress.

## Prerequisites

Before you begin, ensure you have the following installed on your system:

- **Go 1.24+**: Required to compile the project.
- **FFmpeg**: Required on worker nodes for video conversion (with Vulkan support if using GPU acceleration).
- **Vulkan Tools (Optional but Recommended)**: Required on worker nodes for hardware acceleration detection.

## 1. Clone the Repository

Clone the repository and enter the directory:

```bash
git clone https://github.com/darkace1998/video-converter-ecosystem.git
cd video-converter-ecosystem
```

## 2. Build the Binaries

The project consists of three main runnable components: the Master coordinator, the Worker, and the CLI. Build them using the following commands:

```bash
# Build Master
cd video-converter-master
go build -o master
cd ..

# Build Worker
cd video-converter-worker
go build -o worker
cd ..

# Build CLI
cd video-converter-cli
go build -o video-converter-cli
cd ..
```

## 3. Set Up the Master Coordinator

The Master node acts as the central coordinator, job queue manager, and state tracker.

1. Navigate to the `video-converter-master` directory:
   ```bash
   cd video-converter-master
   ```

2. Create a configuration file by copying the example:
   ```bash
   cp config.yaml.example config.yaml
   ```

3. Open `config.yaml` and configure the directories you want to scan for videos and where you want to output the converted files:
   ```yaml
   scanner:
     root_path: /path/to/your/source/videos
     output_base: /path/to/your/output/videos
   ```
   *Make sure these directories exist on the Master node.*

   *(Optional) If you want to secure your cluster, set an `api_key` under the `server` block in `config.yaml`.*

4. Start the Master server:
   ```bash
   ./master --config config.yaml
   ```
   *By default, the Master listens on `http://0.0.0.0:8080`.*

## 4. Set Up a Worker Node

Worker nodes connect to the Master coordinator to fetch and process jobs. Thanks to **Dynamic Worker Configuration**, they can automatically fetch their settings from the Master.

1. Open a new terminal and navigate to the `video-converter-worker` directory:
   ```bash
   cd video-converter-worker
   ```

2. Start the worker, pointing it to your Master server's URL:
   ```bash
   # If your master server has an api_key configured, provide it via the environment variable:
   DARKSTREAM_API_KEY="your_api_key_here" ./worker -url http://localhost:8080

   # Or without auth:
   ./worker -url http://localhost:8080
   ```
   *(Replace `localhost` with the Master's IP address if running on a different machine.)*

   *Tip: The worker automatically starts a local diagnostics server (e.g., at `127.0.0.1:45321`). Check the worker startup logs for the exact URL to view its health and metrics.*

The worker will automatically detect its hardware capabilities (e.g., Vulkan GPUs), register with the Master, fetch the conversion configuration, and start pulling jobs if any source videos were found by the Master.

## 5. Monitoring and Management

DarkStream provides two main ways to monitor your cluster.

### Using the Web Dashboard

The Master coordinator serves a built-in Web UI. Open your web browser and navigate to:

```
http://localhost:8080/
```

Here you can view real-time system status, worker metrics, and recent job activities.

### Using the CLI

You can also use the `video-converter-cli` to interact with the cluster.

1. Open a new terminal and navigate to the `video-converter-cli` directory.
2. Check the overall conversion progress (if auth is enabled, set `export DARKSTREAM_API_KEY="your_key"` first):
   ```bash
   ./video-converter-cli status --master-url http://localhost:8080
   ```
3. View detailed statistics about jobs and active workers:
   ```bash
   ./video-converter-cli stats --master-url http://localhost:8080 --detailed
   ```

## Next Steps

- Check out the [Configuration Guide](CONFIGURATION.md) for more advanced configuration options.
- Review the [API Reference](API.md) to learn how to integrate programmatically.
- Read the [Architecture Overview](ARCHITECTURE.md) for a deeper understanding of how DarkStream works.
