package recording

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sync"

	"github.com/gorilla/websocket"
)

const (
	obsWebSocketURL = "ws://localhost:4444"
)

type OBSWebSocketClient struct {
	conn          *websocket.Conn
	mu            sync.Mutex
	requestID     int
	currentOpChan chan error
}

func NewOBSWebSocketClient() *OBSWebSocketClient {
	return &OBSWebSocketClient{
		currentOpChan: make(chan error, 1),
	}
}

func (client *OBSWebSocketClient) Connect() error {
	u, err := url.Parse(obsWebSocketURL)
	if err != nil {
		return fmt.Errorf("failed to parse URL: %w", err)
	}

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to connect to OBS WebSocket: %w", err)
	}

	client.conn = conn
	go client.listen()
	return client.sendIdentify()
}

func (client *OBSWebSocketClient) sendIdentify() error {
	identify := map[string]interface{}{
		"op": 1,
		"d": map[string]interface{}{
			"rpcVersion": 1,
		},
	}
	return client.sendMessage(identify)
}

func (client *OBSWebSocketClient) sendMessage(message map[string]interface{}) error {
	client.mu.Lock()
	defer client.mu.Unlock()

	client.requestID++
	message["d"].(map[string]interface{})["requestId"] = fmt.Sprintf("%d", client.requestID)
	return client.conn.WriteJSON(message)
}

func (client *OBSWebSocketClient) listen() {
	for {
		_, message, err := client.conn.ReadMessage()
		if err != nil {
			client.currentOpChan <- fmt.Errorf("read error: %w", err)
			return
		}

		var response map[string]interface{}
		if err := json.Unmarshal(message, &response); err != nil {
			client.currentOpChan <- fmt.Errorf("unmarshal error: %w", err)
			return
		}

		client.handleMessage(response)
	}
}

func (client *OBSWebSocketClient) handleMessage(message map[string]interface{}) {
	opCode := int(message["op"].(float64))
	if opCode == 2 {
		client.currentOpChan <- nil
	} else if opCode == 7 {
		status := message["d"].(map[string]interface{})["requestStatus"].(map[string]interface{})
		if int(status["code"].(float64)) == 100 {
			client.currentOpChan <- nil
		} else {
			client.currentOpChan <- fmt.Errorf("operation failed: %s", status["comment"].(string))
		}
	}
}

func (client *OBSWebSocketClient) TriggerHotkey(keyID string) error {
	request := map[string]interface{}{
		"op": 6,
		"d": map[string]interface{}{
			"requestType": "TriggerHotkeyByKeySequence",
			"requestData": map[string]interface{}{
				"keyId": keyID,
			},
		},
	}
	if err := client.sendMessage(request); err != nil {
		return err
	}
	return <-client.currentOpChan
}

func (client *OBSWebSocketClient) Close() error {
	return client.conn.Close()
}
