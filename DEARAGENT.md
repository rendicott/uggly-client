# Headless Mode Integration Testing - Status Report

## Objective
Enable non-interactive automated testing of uggly-client (TUI browser) by:
- Injecting synthetic keystrokes (`-key` flag)
- Adding delays (`-delay` flag)
- Running without a real terminal (`--headless` mode)
- Capturing colored ASCII screen output (`--output` flag)

## Current Status

### What Works ✅
1. **CLI Flag Parsing**: `-headless`, `-auto-exit`, `-timeout`, `-output` flags are correctly parsed
2. **Simulation Screen**: `tcell.NewSimulationScreen("UTF-8")` initializes correctly
3. **Exit Mechanism**: F10 injection via `postF10()` triggers `exit()` correctly
4. **Basic Screen Capture**: `ScreenOutput()` function exists and can capture a pre-rendered screen
5. **Program Flow**: The program starts, runs, and exits without crashing

### What Doesn't Work ❌
1. **Screen Capture Output**: The captured screen is always empty (0 non-space cells)
2. **Page Loading**: The page content from the server is not appearing in the captured output
3. **Timing**: The capture happens before the page has a chance to load/render

## What Was Tried During Debugging

### 1. Debug Output Injection
Added extensive debug logging throughout the exit and capture flow:
- `[EXIT] exit() called`
- `[EXIT] exitFlag set`
- `[EXIT] headless mode, calling capture`
- `[CAPTURE] headlessCaptureScreen entered`
- File writes at critical points to track execution

**Finding**: The program enters `headlessCaptureScreen()` but the screen buffer is empty.

### 2. Time.Sleep() Investigation
Tested if `time.Sleep(200ms)` was causing the program to exit:
- Replaced with `<-time.After(200 * time.Millisecond)`
- Added file writes before and after sleep
- Ran without `timeout` command

**Finding**: Program does exit during sleep, but this is expected behavior - `os.Exit()` is called after capture.

### 3. Goroutine Race Condition Check
Investigated if `os.Exit()` was being called twice:
- Added `exitFlag` check at start of `exit()`
- Verified only one call to `os.Exit()`

**Finding**: No double exit. The issue is that the screen is empty when captured.

### 4. Signal Handler Test
Installed signal handlers to catch unexpected termination:
```go
signal.Notify(c, os.Interrupt, syscall.SIGTERM)
```

**Finding**: No signals received. Program exits normally via `os.Exit()`.

### 5. Minimal Test Case
Created standalone test with tcell SimulationScreen:
```go
s := tcell.NewSimulationScreen("UTF-8")
s.Init()
// Set content
s.Show()
time.Sleep(500 * time.Millisecond)
// Capture
cells, w, h := sim.GetContents()
```

**Finding**: The minimal test works perfectly. The issue is specific to uggly-client's page loading/rendering flow.

### 6. Server Verification
Verified puggly-server is running and responding:
```bash
curl -s http://localhost:4443/  # Returns HTML correctly
```

**Finding**: Server is working. Issue is in the client's page loading/rendering.

## What I Think Is Broken

### Root Cause: Timing Issue Between Page Load and Capture

The `startHeadlessTimer()` function waits for synthetic steps to drain, then injects F10 after a timeout. However:

1. **No synthetic steps** = queue drains immediately
2. **Timer injects F10** after `exitDelay` seconds (from `--timeout` flag)
3. **BUT**: The initial page load (`get2()`) is asynchronous and may not complete before F10 is injected
4. **Result**: Screen capture happens before page content is rendered

### Code Flow Analysis

```
main()
  └─> brow.start(ugri, headless)
       ├─> go b.startHeadlessTimer()  // Starts counting down
       ├─> go b.pollEvents(ctx)       // Waits for events
       └─> b.get2(ctx, linkRequest)   // ASYNC page load
       
startHeadlessTimer()
  └─> Wait for queue drain (immediate if no -key flags)
  └─> Sleep(exitDelay)  // e.g., 15 seconds
  └─> postF10()         // Inject F10

pollEvents()
  └─> Receive F10
  └─> b.exit(0)
       └─> b.headlessCaptureScreen()  // SCREEN IS STILL EMPTY!
```

The problem: Even with `--timeout 15`, the page load might fail or hang, leaving the screen empty when captured.

## Evidence

1. **Empty screen buffer**: `ScreenOutput()` returns 0 non-space cells
2. **No error messages**: The program doesn't report connection failures
3. **No content rendering**: Menu bar renders but page content does not

## Recommendations for Next Agent

### Immediate Fixes to Try

1. **Add explicit delay before F10 injection**:
   ```go
   // In startHeadlessTimer(), add initial delay
   time.Sleep(3 * time.Second)  // Give page time to load
   ```

2. **Check if page loaded before capturing**:
   ```go
   // In headlessCaptureScreen(), check b.currentPage
   if b.currentPage == nil || b.currentPage.Name == "" {
       fmt.Println("WARNING: No page loaded, screen will be empty")
   }
   ```

3. **Add connection timeout logging**:
   Check if `get2()` is actually succeeding or failing silently

4. **Test with explicit navigation**:
   ```bash
   ./ugglyc --headless --timeout 20 -key F1 -delay 2s -key Enter --output out.txt ugtp://localhost:4443/
   ```

### Alternative Approaches

1. **Use `--auto-exit` with `-delay`**:
   ```bash
   ./ugglyc --headless --auto-exit -delay 5s --output out.txt ugtp://localhost:4443/
   ```

2. **Add a "wait for page" mechanism**:
   Track when `b.currentPage` is set and only capture after that

3. **Render menu first, then navigate**:
   Ensure the initial menu renders before attempting page load

### Files to Examine

- `uggcli.go:1344-1410` - `start()` function flow
- `uggcli.go:459-480` - `startHeadlessTimer()` logic
- `uggcli.go:57-92` - `headlessCaptureScreen()` implementation
- `uggcli.go:540-660` - `get2()` page loading logic

### Test Commands

```bash
# Start server
cd /home/rusty/src/uggly-client/puggly-server
python3 -m http.server 4443 &

# Test 1: Basic headless (likely fails - no time to load)
./ugglyc --headless --auto-exit --output /tmp/test1.txt http://localhost:4443/

# Test 2: With timeout (should give more time)
./ugglyc --headless --timeout 10 --output /tmp/test2.txt http://localhost:4443/

# Test 3: With explicit delay
./ugglyc --headless --auto-exit -delay 5s --output /tmp/test3.txt http://localhost:4443/

# Test 4: Navigate after load
./ugglyc --headless --timeout 15 -delay 2s -key F1 -delay 1s -key Enter --output /tmp/test4.txt http://localhost:4443/
```

## Code Changes Made

No permanent code changes were made. All debug output was added temporarily and reverted.

## Environment

- Working directory: `/home/rusty/src/uggly-client`
- Server: `puggly-server` on `http://localhost:4443`
- Go version: 1.21+
- tcell version: v2.7.4

---

*Report generated: 2026-07-10*
*Agent: opencode*
