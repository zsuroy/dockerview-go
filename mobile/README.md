# DockerView Client (Expo)

This is a beautiful, modern cross-platform mobile and web client for **DockerView-Go**, built using Expo SDK 57, React Native, and Expo Router.

It allows you to monitor and control your Docker containers directly from your mobile device (iOS/Android) or standard web browser.

---

## 🚀 Features

- **Real-time Container Dashboard**: Displays all containers, statuses, ports, CPU, and Memory usage with auto-polling.
- **Group Statistics**: Overview cards showing Total, Active (Running), Stopped, and Warning counts.
- **Lifecycle Control**: Start, Stop, and Restart docker containers directly from your phone.
- **Real-time Logs Console**: View container logs with support for log level filters (`ALL`, `INFO`, `WARN`, `ERROR`), tail line counts, and keyword `grep` filtering.
- **Exec Shell Console**: Execute custom shell commands (e.g., `df -h`, `env`, `ls`) inside the running containers and inspect stdout/stderr outputs. Predefined templates are also available for quick execution.
- **Security Token Protection**: Safely logs in and saves server IP configurations and authentication tokens locally using `AsyncStorage`.

---

## 🛠️ Get Started

### 1. Pre-requisites: Start DockerView-Go Server

Make sure your DockerView-Go server is running with the HTTP server option:
```bash
./dockerview -server -port 8080 -token YOUR_SECURE_TOKEN
```

### 2. Set Up the Mobile Client

1. **Install Dependencies**
   Navigate to the `mobile` folder and install dependencies:
   ```bash
   cd mobile
   npm install
   ```

2. **Start the Development Server**
   ```bash
   npx expo start
   ```

3. **Open the App**
   - **Android Emulator**: Press `a` in the terminal to launch.
   - **iOS Simulator**: Press `i` in the terminal to launch.
   - **Expo Go (Physical Device)**: Scan the QR code displayed in the terminal using the Expo Go app.
   - **Web Browser**: Press `w` in the terminal.

---

## ⚙️ Configuration

Open the **Settings** tab in the app to configure your connection:
- **Server URL**:
  - For physical iOS/Android devices: Enter the local IP of your computer (e.g., `http://192.168.1.100:8080`).
  - For Android Emulator: Use `http://10.0.2.2:8080` (special loopback alias to host machine).
  - For iOS Simulator or Web: Use `http://localhost:8080`.
- **Security Token**: Enter the `-token` specified when launching `dockerview`. (Leave blank if you didn't configure a token).
- Press **Save Changes** and click **Test Connection** to verify your setup.

---

## 🔧 Build a standalone APK

### Prerequisites

- Node.js 20+
- Android Studio (for Android SDK)
- Java 17+ (bundled with Android Studio)

Set up Java environment:

```bash
# macOS (Android Studio bundled JDK)
export JAVA_HOME="/Applications/Android Studio.app/Contents/jbr/Contents/Home"
export PATH="$JAVA_HOME/bin:$PATH"
```

Add these lines to `~/.zshrc` to make them permanent.

### 1. Configure Android SDK

Create `android/local.properties` pointing to your SDK:

```bash
echo "sdk.dir=$HOME/Library/Android/sdk" > android/local.properties
```

### 2. Allow HTTP (cleartext) traffic

Android 9+ blocks HTTP by default. This is configured via `expo-build-properties` in [`app.json`](app.json):

```json
"plugins": [
  [
    "expo-build-properties",
    { "android": { "usesCleartextTraffic": true } }
  ]
]
```

### 3. Prebuild & compile

```bash
# Generate the native Android project
npx expo prebuild --platform android --clean

# Compile release APK
cd android
./gradlew assembleRelease --parallel --build-cache

# APK location: android/app/build/outputs/apk/release/
```

### Speed up builds

Use Gradle daemon and parallel execution for faster incremental builds:

```bash
cd android
./gradlew assembleRelease --parallel --build-cache --daemon --max-workers=8
```

- `--parallel` — run tasks in parallel across modules
- `--build-cache` — cache build outputs across runs
- `--daemon` — keep a warm JVM process for subsequent builds
- `--max-workers=8` — use more concurrent workers

### Memory tuning

Gradle can run out of memory on large builds. Increase the JVM heap in [`android/gradle.properties`](android/gradle.properties):

```properties
org.gradle.jvmargs=-Xmx8g -XX:MaxMetaspaceSize=2048m
```

Also reduce the architectures you build for if testing locally:

```properties
reactNativeArchitectures=arm64-v8a
```

### Skip the device check

`npx expo run:android` requires a connected device. Use gradle directly to compile without one (as shown above).

### Java version issues

If you see `Daemons could not be reused`, stop all daemons and retry:

```bash
cd android && ./gradlew --stop
```

### Need to re-prebuild?

If the `android/` directory gets corrupted, clean and regenerate:

```bash
rm -rf android
npx expo prebuild --platform android --clean
```
