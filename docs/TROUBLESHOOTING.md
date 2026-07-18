# Troubleshooting Guide

This document provides solutions for common issues encountered when setting up or running the DarkStream distributed video converter system.

## Table of Contents

1. [Worker Connection Issues](#worker-connection-issues)
2. [Hardware Acceleration (Vulkan) Issues](#hardware-acceleration-vulkan-issues)
3. [FFmpeg Processing Errors](#ffmpeg-processing-errors)
4. [Master/Database Issues](#masterdatabase-issues)
5. [CLI Issues](#cli-issues)

---

## Worker Connection Issues

### "Error connecting to master server" / Connection Refused

**Symptoms:**
- The worker or CLI outputs "Error connecting to master server".
- HTTP request errors are logged.

**Solutions:**
1. **Verify Master is Running:** Check the master node logs or test its liveness probe:
   ```bash
   curl http://<master-ip>:8080/healthz
   ```
2. **Check the Master URL:** Ensure the worker or CLI is pointing to the correct URL, including the protocol and port (e.g., `http://localhost:8080`).
3. **Firewall/Network:** Ensure port `8080` (or your configured master port) is open on the master node and accessible from the worker nodes.

### Authentication Failures (HTTP 401)

**Symptoms:**
- The worker logs show "401 Unauthorized" when trying to fetch jobs or configurations.

**Solutions:**
1. **API Key Mismatch:** If the master is configured with an `api_key` in `config.yaml`, the worker must provide the identical key.
2. **Provide the Key:** Provide the key via the `DARKSTREAM_API_KEY` environment variable when starting the worker or CLI:
   ```bash
   export DARKSTREAM_API_KEY="your_api_key_here"
   ```

### Worker Paused

**Symptoms:**
- The worker connects successfully but receives no jobs (HTTP 204 No Content), even though the master has pending jobs.

**Solutions:**
1. **Check Global Queue Status:** The global job queue might be paused. Resume it using the CLI:
   ```bash
   video-converter-cli queue-resume --master-url http://localhost:8080
   ```
2. **Check Individual Worker Status:** The specific worker might be paused. Check `video-converter-cli workers` and resume if necessary:
   ```bash
   video-converter-cli worker-resume --master-url http://localhost:8080 --worker-id <worker-id>
   ```

---

## Hardware Acceleration (Vulkan) Issues

### Vulkan Not Detected

**Symptoms:**
- The worker starts but logs indicate Vulkan is disabled or no devices were found.
- Conversions run slowly using the CPU.

**Solutions:**
1. **Install Vulkan Drivers/Tools:** Ensure Vulkan user-space drivers and tools are installed on the worker node.
   - Ubuntu/Debian: `sudo apt-get install vulkan-tools libvulkan1 mesa-vulkan-drivers`
   - Test locally with `vulkaninfo --summary`.
2. **Check FFmpeg Build:** Your FFmpeg binary must be compiled with Vulkan support.
   - Run `ffmpeg -hwaccels` and look for `vulkan` in the output list.
3. **Check Worker Config:** Ensure `ffmpeg.use_vulkan` is set to `true` (or inherited from the master defaults).

---

## FFmpeg Processing Errors

### "ffmpeg: command not found"

**Symptoms:**
- Worker logs "executable file not found in $PATH".

**Solutions:**
1. **Install FFmpeg:**
   - Ubuntu/Debian: `sudo apt-get install ffmpeg`
   - macOS: `brew install ffmpeg`
2. **Configure Path:** If FFmpeg is installed in a non-standard location, set the explicit path in the worker's `config.yaml`:
   ```yaml
   ffmpeg:
     path: /path/to/custom/ffmpeg
   ```

### Job Repeatedly Fails During Conversion

**Symptoms:**
- A job fails repeatedly, its retry count increments, and it is eventually marked as `failed`.

**Solutions:**
1. **Check Worker Logs:** The worker captures the standard error output from FFmpeg. Check the worker log file (e.g., `worker.log`) for the specific FFmpeg error message.
2. **Unsupported Codecs:** The input file might use an exotic codec that your FFmpeg build doesn't support.
3. **File Corruption:** The source video file might be corrupt. Try playing it locally to verify.
4. **Inspect the Job:** Use the CLI to check the error message recorded by the master:
   ```bash
   video-converter-cli job --job-id <id>
   ```

---

## Master/Database Issues

### SQLite Database Locked

**Symptoms:**
- The master node logs errors like `database is locked`.

**Solutions:**
1. **Storage Performance:** SQLite requires reasonably fast underlying storage. Avoid running the master database on extremely slow or high-latency network drives (e.g., NFS without proper locking support).
2. **Concurrency Overload:** If you have an unusually high number of workers causing contention, consider tuning the master API rate limits.
3. **Restart Master:** As a last resort, restarting the master can clear temporary file locks. Job state is persistent and will be recovered.

### Scanner Not Finding Files

**Symptoms:**
- The master starts, but no jobs are added to the pending queue.

**Solutions:**
1. **Verify Root Path:** Ensure `scanner.root_path` in the master's `config.yaml` points to the correct, existing directory.
2. **Check Extensions:** Ensure the video file extensions (e.g., `.mp4`, `.mkv`) are listed in `scanner.video_extensions`.
3. **Check Depth limit:** If files are in subdirectories, make sure `scanner.recursive_depth` is set to `-1` (unlimited) or a large enough positive integer.

---

## CLI Issues

### Command Fails Immediately

**Symptoms:**
- The CLI command returns an error without making a network request.

**Solutions:**
1. **Check Usage:** Ensure all required flags are provided. Use `video-converter-cli <command> --help` to see expected arguments.
2. **Invalid URL:** Ensure the `--master-url` is fully qualified (e.g., `http://localhost:8080`, not just `localhost:8080`).
