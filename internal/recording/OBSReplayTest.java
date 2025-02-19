package app.owlcms.replay;

import java.io.File;
import java.io.FileReader;
import java.io.FileWriter;
import java.io.IOException;
import java.net.URI;
import java.nio.file.Files;
import java.util.Vector;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

import org.java_websocket.client.WebSocketClient;
import org.java_websocket.handshake.ServerHandshake;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import com.google.gson.Gson;
import com.google.gson.JsonObject;
import com.jcraft.jsch.Channel;
import com.jcraft.jsch.ChannelSftp;
import com.jcraft.jsch.JSch;
import com.jcraft.jsch.Session;

public class OBSReplayTest {
    private static final Logger logger = LoggerFactory.getLogger(OBSReplayTest.class);
    private static final String OBS_WEBSOCKET_URL = "ws://localhost:4444";
    private static final Gson gson = new Gson();
    private final WebSocketClient webSocketClient;
    private final CountDownLatch connectLatch = new CountDownLatch(1);
    private CompletableFuture<Void> currentOperation = new CompletableFuture<>();
    private int requestCounter = 0;

    private static final String SFTP_HOST = System.getProperty("sftpHost", "localhost");
    private static final int SFTP_PORT = 2022;
    private static final String SFTP_USER = "replays";
    private static final String SFTP_PASSWORD = "replays";
    private static final String LOCAL_REPLAY_DIR = "replays";
    private static final String SEQUENCE_FILE = "replay-sequence.txt";
    private static final boolean DEBUG_FFMPEG = Boolean.getBoolean("debugFfmpeg");

    public OBSReplayTest() {
        this.webSocketClient = createWebSocketClient();
    }

    public static void main(String[] args) {
        OBSReplayTest test = new OBSReplayTest();
        test.execute();
    }

    private void execute() {
        try {
            webSocketClient.connect();
            if (!connectLatch.await(5, TimeUnit.SECONDS)) {
                logger.error("Failed to connect to OBS");
                System.exit(1);
            }

            // Execute operations sequentially
            triggerHotkey("OBS_KEY_F6")
                .thenCompose(v -> delay(2000))
                .thenCompose(v -> triggerHotkey("OBS_KEY_F7"))
                .thenCompose(v -> delay(2000))
                .thenCompose(v -> triggerHotkey("OBS_KEY_F8"))
                .thenCompose(v -> delay(250))
                .thenCompose(v -> triggerHotkey("OBS_KEY_F6"))
                .thenCompose(v -> delay(5000))
                .thenCompose(v -> downloadReplays())  // Returns the sequence number
                .thenCompose(this::processReplays)    // Process the downloaded files
                .get();

            logger.info("Replay test, download and processing completed successfully");
            System.exit(0);
        } catch (Exception e) {
            logger.error("Failed to execute replay test", e);
            System.exit(1);
        }
    }

    private WebSocketClient createWebSocketClient() {
        return new WebSocketClient(URI.create(OBS_WEBSOCKET_URL)) {
            @Override
            public void onOpen(ServerHandshake handshake) {
                logger.info("Connected to OBS WebSocket");
                sendIdentify();
            }

            @Override
            public void onMessage(String message) {
                try {
                    JsonObject json = gson.fromJson(message, JsonObject.class);
                    handleMessage(json);
                } catch (Exception e) {
                    currentOperation.completeExceptionally(e);
                }
            }

            @Override
            public void onClose(int code, String reason, boolean remote) {
                logger.info("Connection closed: {}", reason);
                currentOperation.completeExceptionally(new RuntimeException("Connection closed: " + reason));
            }

            @Override
            public void onError(Exception ex) {
                logger.error("WebSocket error", ex);
                currentOperation.completeExceptionally(ex);
            }
        };
    }

    private void sendIdentify() {
        JsonObject identify = new JsonObject();
        identify.addProperty("op", 1);
        JsonObject d = new JsonObject();
        d.addProperty("rpcVersion", 1);
        identify.add("d", d);
        webSocketClient.send(gson.toJson(identify));
    }

    private void handleMessage(JsonObject json) {
        int opCode = json.get("op").getAsInt();
        if (opCode == 2) {
            // Identified successfully
            connectLatch.countDown();
        } else if (opCode == 7) {
            // Response to a request
            JsonObject d = json.getAsJsonObject("d");
            if (d != null && d.has("requestStatus")) {
                JsonObject status = d.getAsJsonObject("requestStatus");
                if (status.get("code").getAsInt() == 100) {
                    currentOperation.complete(null);
                } else {
                    currentOperation.completeExceptionally(
                        new RuntimeException("Operation failed: " + status.get("comment").getAsString()));
                }
            }
        }
    }

    private CompletableFuture<Void> triggerHotkey(String keyId) {
        logger.info("Triggering key: {}", keyId);
        currentOperation = new CompletableFuture<>();
        JsonObject request = new JsonObject();
        request.addProperty("op", 6);
        
        JsonObject data = new JsonObject();
        data.addProperty("requestType", "TriggerHotkeyByKeySequence");
        data.addProperty("requestId", String.valueOf(++requestCounter));
        
        // Create proper request data structure
        JsonObject requestData = new JsonObject();
        requestData.addProperty("keyId", keyId);
        data.add("requestData", requestData);
        
        request.add("d", data);
        String jsonRequest = gson.toJson(request);
        logger.trace("Sending request: {}", jsonRequest);
        webSocketClient.send(jsonRequest);
        return currentOperation;
    }

    private CompletableFuture<Void> delay(long ms) {
        return CompletableFuture.runAsync(() -> {
            try {
                Thread.sleep(ms);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
            }
        });
    }

    private int getNextSequenceNumber() {
        File seqFile = new File(SEQUENCE_FILE);
        int nextNum = 1;
        if (seqFile.exists()) {
            try (FileReader reader = new FileReader(seqFile)) {
                nextNum = Integer.parseInt(Files.readString(seqFile.toPath()).trim()) + 1;
            } catch (IOException e) {
                logger.warn("Could not read sequence file, starting at 1", e);
            }
        }
        try (FileWriter writer = new FileWriter(seqFile)) {
            writer.write(String.valueOf(nextNum));
        } catch (IOException e) {
            logger.error("Could not write sequence file", e);
        }
        return nextNum;
    }

    private void renameWithRetry(ChannelSftp sftpChannel, String oldName, String newName, int maxRetries) throws Exception {
        int attempt = 0;
        while (attempt < maxRetries) {
            try {
                sftpChannel.rename(oldName, newName);
                logger.debug("Renamed on server: {} to {}", oldName, newName);
                return;
            } catch (Exception e) {
                attempt++;
                if (attempt >= maxRetries) {
                    throw new Exception("Failed to rename after " + maxRetries + " attempts: " + e.getMessage());
                }
                logger.warn("Rename attempt {} failed, retrying in 1s: {}", attempt, e.getMessage());
                Thread.sleep(1000);
            }
        }
    }

    private void trimVideo(File inputFile) throws Exception {
        String outputPath = inputFile.getPath().replace(".flv", ".mp4");
        ProcessBuilder pb = new ProcessBuilder(
            "ffmpeg", 
            DEBUG_FFMPEG ? "" : "-v", // Use -v for more complete quiet mode
            DEBUG_FFMPEG ? "" : "quiet", // Complete silence
            DEBUG_FFMPEG ? "" : "-stats", // No stats output
            "-i", inputFile.getPath(),
            "-ss", "1.0",
            "-c:v", "libx264",
            "-c:a", "aac",
            "-movflags", "+faststart",
            outputPath
        );
        
        pb.command().removeIf(String::isEmpty);
        
        if (DEBUG_FFMPEG) {
            pb.inheritIO();
        } else {
            pb.redirectError(ProcessBuilder.Redirect.DISCARD);
            pb.redirectOutput(ProcessBuilder.Redirect.DISCARD);
        }
        
        logger.info("Converting and trimming video: {}", inputFile.getName());
        Process p = pb.start();
        int exitCode = p.waitFor();
        
        if (exitCode == 0) {
            // Delete original FLV file after successful conversion
            if (inputFile.delete()) {
                logger.debug("Successfully converted: {} to {}", 
                    inputFile.getName(), new File(outputPath).getName());
            } else {
                logger.warn("Could not delete original file: {}", inputFile.getName());
            }
        } else {
            throw new Exception("FFmpeg failed with exit code: " + exitCode);
        }
    }

    private CompletableFuture<Integer> downloadReplays() {
        return CompletableFuture.supplyAsync(() -> {
            try {
                File localDir = new File(LOCAL_REPLAY_DIR);
                if (!localDir.exists()) {
                    localDir.mkdir();
                    logger.info("Created local directory: {}", localDir.getAbsolutePath());
                }

                int nextNum = getNextSequenceNumber();
                
                // Setup JSch
                JSch jsch = new JSch();
                Session session = jsch.getSession(SFTP_USER, SFTP_HOST, SFTP_PORT);
                session.setPassword(SFTP_PASSWORD);
                session.setConfig("StrictHostKeyChecking", "no");
                session.connect();

                Channel channel = session.openChannel("sftp");
                channel.connect();
                ChannelSftp sftpChannel = (ChannelSftp) channel;

                // Get list of .flv files
                @SuppressWarnings("unchecked")
                Vector<ChannelSftp.LsEntry> list = sftpChannel.ls("*.flv");
                
                // Log files present on server
                logger.trace("Files present on server:");
                list.forEach(entry -> logger.trace("  {} ({}bytes)", 
                    entry.getFilename(), entry.getAttrs().getSize()));
                
                logger.info("Starting rename of {} files with sequence {}", list.size(), nextNum);

                // First rename all files on server
                for (ChannelSftp.LsEntry entry : list) {
                    String filename = entry.getFilename();
                    String numberedFilename = String.format("%03d-%s", nextNum, filename);
                    try {
                        renameWithRetry(sftpChannel, filename, numberedFilename, 10);
                    } catch (Exception e) {
                        logger.error("Could not rename on server after retries: {} - {}", filename, e.getMessage());
                        throw new RuntimeException(e);
                    }
                }

                // Download and remove renamed files
                @SuppressWarnings("unchecked")
                Vector<ChannelSftp.LsEntry> renamedList = sftpChannel.ls(String.format("%03d-*.flv", nextNum));
                for (ChannelSftp.LsEntry entry : renamedList) {
                    String filename = entry.getFilename();
                    File localFile = new File(localDir, filename);
                    if (!localFile.exists()) {
                        logger.info("Downloading: {}", filename);
                        sftpChannel.get(filename, localFile.getAbsolutePath());
                        // Remove remote file after successful download
                        sftpChannel.rm(filename);
                        logger.debug("Removed from server: {}", filename);
                    } else {
                        logger.debug("Skipping existing file: {}", filename);
                        // Remove remote file even if local exists
                        sftpChannel.rm(filename);
                        logger.debug("Removed from server: {}", filename);
                    }
                }

                sftpChannel.exit();
                session.disconnect();

                // Return the sequence number instead of void
                return nextNum;
            } catch (Exception e) {
                throw new RuntimeException("Failed to download replays: " + e.getMessage(), e);
            }
        });
    }

    private CompletableFuture<Void> processReplays(int sequenceNum) {
        return CompletableFuture.runAsync(() -> {
            try {
                logger.info("Processing downloaded videos...");
                File[] downloadedFiles = new File(LOCAL_REPLAY_DIR)
                    .listFiles((dir, name) -> name.startsWith(String.format("%03d-", sequenceNum)));
                
                if (downloadedFiles != null) {
                    for (File file : downloadedFiles) {
                        try {
                            trimVideo(file);
                        } catch (Exception e) {
                            logger.error("Failed to trim video {}: {}", file.getName(), e.getMessage());
                        }
                    }
                }
                logger.info("Processing completed successfully");
            } catch (Exception e) {
                throw new RuntimeException("Failed to process replays: " + e.getMessage(), e);
            }
        });
    }
}
