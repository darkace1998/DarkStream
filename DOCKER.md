# Docker Setup for DarkStream

This guide explains how to build and run the DarkStream distributed video converter using Docker and Docker Compose.

## Quick Start

### Using Docker Compose (Recommended)

```bash
# Build and start all services
docker-compose up -d

# View logs
docker-compose logs -f

# Check service status
docker-compose ps

# Stop services
docker-compose down

# Stop and remove volumes
docker-compose down -v
```

### Verify Services Are Running

```bash
# Check master service
curl http://localhost:8080/health

# View master logs
docker-compose logs master

# View worker logs
docker-compose logs worker1

# Execute CLI commands inside the container
docker-compose exec cli ./video-converter-cli status --master http://master:8080
```

## Building Individual Services

### Build Only Master
```bash
docker build --target master -t darkstream:master .
```

### Build Only Worker
```bash
docker build --target worker -t darkstream:worker .
```

### Build Only CLI
```bash
docker build --target cli -t darkstream:cli .
```

### Build All-in-One Development Image
```bash
docker build --target all-in-one -t darkstream:all-in-one .
```

## Running Individual Services

### Run Master Service
```bash
docker run -d \
  --name darkstream-master \
  -p 8080:8080 \
  -v videos:/mnt/storage/videos \
  -v master_db:/app/db \
  -e LOG_LEVEL=info \
  darkstream:master
```

### Run Worker Service
```bash
docker run -d \
  --name darkstream-worker-1 \
  -v videos:/mnt/storage/videos \
  -v worker_tmp:/tmp/worker \
  -e MASTER_HOST=master \
  -e MASTER_PORT=8080 \
  --link darkstream-master:master \
  darkstream:worker
```

### Run CLI Tool
```bash
docker run --rm -it \
  -v videos:/mnt/storage/videos \
  --link darkstream-master:master \
  darkstream:cli status --master http://master:8080
```

## GPU Support

### NVIDIA GPU Setup

The worker service is pre-configured with full NVIDIA GPU support including:

- **Base Image**: `nvidia/cuda:12.6.1-runtime-ubuntu24.04`
- **GPU Libraries**: CUDA runtime, nvidia-utils, libnvidia-gl
- **Vulkan Support**: mesa-vulkan-drivers, libvulkan1, vulkan-tools
- **Graphics Libraries**: Mesa OpenGL, X11 libraries for compatibility

### Requirements

1. **NVIDIA GPU** with compute capability
2. **NVIDIA Docker Runtime** installed on your host:

```bash
# Install NVIDIA Docker runtime (Ubuntu)
curl https://get.docker.com | sh
sudo apt-get install -y nvidia-docker2
sudo systemctl restart docker

# Verify installation
docker run --rm --gpus all nvidia/cuda:12.6.1-runtime-ubuntu24.04 nvidia-smi
```

3. **NVIDIA Drivers** installed on host system

### Using GPU with Docker Compose

GPU support is **enabled by default** in docker-compose.yml. Just run:

```bash
# Build and start with GPU support
docker-compose up -d

# Verify GPU is available in workers
docker-compose exec worker1 nvidia-smi
```

### Running Worker with GPU Only

```bash
docker run --gpus all -d \
  --name darkstream-worker-1 \
  -e NVIDIA_VISIBLE_DEVICES=all \
  -e NVIDIA_DRIVER_CAPABILITIES=compute,utility \
  -v videos:/mnt/storage/videos \
  --network darkstream \
  darkstream:worker
```

### GPU Environment Variables

The following environment variables are set for GPU support:

```dockerfile
NVIDIA_VISIBLE_DEVICES=all          # Expose all GPUs
NVIDIA_DRIVER_CAPABILITIES=compute  # Enable compute capabilities
```

### Testing GPU Access

```bash
# Check GPU availability in worker
docker-compose exec worker1 nvidia-smi

# Verify Vulkan support
docker-compose exec worker1 vulkaninfo

# Check CUDA libraries
docker-compose exec worker1 ldconfig -p | grep cuda
```

### Troubleshooting GPU Issues

**GPU not visible in container:**
```bash
# Check NVIDIA Docker runtime
docker run --rm --gpus all ubuntu nvidia-smi

# If not found, reinstall nvidia-docker
sudo apt-get install --reinstall nvidia-docker2
sudo systemctl restart docker
```

**Out of GPU memory:**
- Check current GPU usage: `docker-compose exec worker1 nvidia-smi`
- Reduce concurrent workers or video resolution
- Monitor with: `watch -n 1 docker-compose exec worker1 nvidia-smi`

**Performance issues:**
- Ensure compute capability ≥ 3.5
- Check nvidia-docker version: `nvidia-docker version`
- Verify driver compatibility: `nvidia-smi`

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `LOG_LEVEL` | `info` | Logging level (debug, info, warn, error) |
| `LOG_FORMAT` | `json` | Log format (json, text) |
| `MASTER_HOST` | `master` | Master service hostname (for workers) |
| `MASTER_PORT` | `8080` | Master service port |

### Config Files

Edit the YAML configuration files and mount them as volumes:

```bash
# Copy example configs to customize them
cp video-converter-master/config.yaml.example config-master.yaml
cp video-converter-worker/config.yaml.example config-worker.yaml

# Edit configurations
nano config-master.yaml
nano config-worker.yaml

# Update docker-compose.yml volumes section to use your custom configs
```

## Docker Network

Services communicate via the `darkstream` bridge network:
- **Master**: Available at `http://master:8080` from worker containers
- **Workers**: Register with master automatically
- **CLI**: Can reach master at `http://master:8080`

## Volumes

| Volume | Purpose |
|--------|---------|
| `videos_storage` | Shared video storage for all services |
| `master_db` | Master service database persistence |
| `worker1_tmp` | Worker 1 temporary work directory |
| `worker2_tmp` | Worker 2 temporary work directory |

## Troubleshooting

### Check service health
```bash
docker-compose ps
docker-compose logs -f master
docker-compose logs -f worker1
```

### Test connectivity between services
```bash
docker-compose exec worker1 curl http://master:8080/health
```

### Rebuild services
```bash
docker-compose build --no-cache
docker-compose up -d
```

### Clean up everything
```bash
docker-compose down -v
docker system prune -a
```

## Performance Tips

1. **Use named volumes** for persistent data (already configured)
2. **Enable BuildKit** for faster builds: `export DOCKER_BUILDKIT=1`
3. **Use `.dockerignore`** to exclude unnecessary files
4. **Pin Go version** to ensure consistency
5. **Multi-stage builds** reduce final image size

## Production Deployment

For production, consider:

1. **Use private Docker registry** instead of public
2. **Add health checks** (already included in docker-compose.yml)
3. **Use proper secrets management** (docker secrets, external vault)
4. **Enable restart policies** (already set to `unless-stopped`)
5. **Monitor resource limits** in docker-compose.yml
6. **Use proper logging drivers** (e.g., splunk, awslogs)
7. **Implement scaling** with orchestration tools (Kubernetes, Docker Swarm)

Example production deployment with resource limits:

```yaml
services:
  master:
    # ... other config ...
    deploy:
      resources:
        limits:
          cpus: '2'
          memory: 2G
        reservations:
          cpus: '1'
          memory: 1G
```

## Additional Resources

- [Docker Documentation](https://docs.docker.com/)
- [Docker Compose Documentation](https://docs.docker.com/compose/)
- [NVIDIA Docker Documentation](https://github.com/NVIDIA/nvidia-docker)
- [FFmpeg Documentation](https://ffmpeg.org/documentation.html)
- [Vulkan Documentation](https://www.khronos.org/vulkan/)
