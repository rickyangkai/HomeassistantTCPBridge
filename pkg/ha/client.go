package ha

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// Message types
const (
	TypeAuthRequired = "auth_required"
	TypeAuth         = "auth"
	TypeAuthOK       = "auth_ok"
	TypeAuthInvalid  = "auth_invalid"
	TypeEvent        = "event"
	TypeResult       = "result"
	TypePing         = "ping"
	TypePong         = "pong"
	TypeCallService  = "call_service"
)

type Client struct {
	url           string
	token         string
	conn          *websocket.Conn
	idCounter     int64
	sendChan      chan interface{}
	onMessage     func(string) // Callback to send data to Savant
	isAuth        bool
	reconnectChan chan bool
	
	mu            sync.RWMutex      // Protects maps and filter
	substituteIDs map[string]string // entity_id -> substitute_id
	idSubstitutes map[string]string // substitute_id -> entity_id
	filter        []string          // attributes filter
}

func NewClient(url, token string, onMessage func(string)) *Client {
	return &Client{
		url:           url,
		token:         token,
		sendChan:      make(chan interface{}, 100),
		onMessage:     onMessage,
		reconnectChan: make(chan bool, 1),
		substituteIDs: make(map[string]string),
		idSubstitutes: make(map[string]string),
		filter:        []string{"all"},
	}
}

func (c *Client) SetFilter(filter []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(filter) == 0 {
		c.filter = []string{"all"}
	} else {
		c.filter = filter
	}
}

func (c *Client) includedWithFilter(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	if len(c.filter) == 0 || (len(c.filter) == 1 && c.filter[0] == "all") {
		return true
	}
	for _, f := range c.filter {
		if f == key {
			return true
		}
	}
	return false
}

func (c *Client) SetSubstituteIDs(subs map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.substituteIDs = subs
	// Rebuild reverse map
	c.idSubstitutes = make(map[string]string)
	for k, v := range subs {
		c.idSubstitutes[v] = k
	}
}

// ResolveID converts a potential substitute ID (from Savant) to a real HA Entity ID
func (c *Client) ResolveID(id string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	if realID, ok := c.idSubstitutes[id]; ok {
		return realID
	}
	return id
}

func (c *Client) getSubstituteID(entityID string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	if val, ok := c.substituteIDs[entityID]; ok {
		return val
	}
	return ""
}

func (c *Client) Start() {
	go c.connectLoop()
}

func (c *Client) connectLoop() {
	for {
		err := c.connect()
		if err != nil {
			log.Printf("HA: Connection failed: %v. Retrying in 5s...", err)
			time.Sleep(5 * time.Second)
			continue
		}

		// Wait for disconnect signal
		<-c.reconnectChan
		log.Println("HA: Disconnected, reconnecting...")
		c.cleanup()
		time.Sleep(1 * time.Second)
	}
}

func (c *Client) connect() error {
	log.Printf("HA: Connecting to %s", c.url)
	conn, _, err := websocket.DefaultDialer.Dial(c.url, nil)
	if err != nil {
		return err
	}
	c.conn = conn
	c.isAuth = false

	go c.readLoop()
	go c.writeLoop()
	go c.pingLoop()

	return nil
}

func (c *Client) cleanup() {
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

func (c *Client) readLoop() {
	defer func() {
		c.reconnectChan <- true
	}()

	for {
		if c.conn == nil {
			return
		}
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			log.Printf("HA: Read error: %v", err)
			return
		}
		c.handleMessage(message)
	}
}

func (c *Client) writeLoop() {
	for msg := range c.sendChan {
		if c.conn == nil {
			continue
		}
		err := c.conn.WriteJSON(msg)
		if err != nil {
			log.Printf("HA: Write error: %v", err)
			return
		}
	}
}

func (c *Client) pingLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if c.conn != nil && c.isAuth {
				c.SendCommand(map[string]interface{}{
					"type": TypePing,
				})
			}
		case <-c.reconnectChan:
			return
		}
	}
}

func (c *Client) SendCommand(cmd map[string]interface{}) {
	id := atomic.AddInt64(&c.idCounter, 1)
	cmd["id"] = id
	c.sendChan <- cmd
}

func (c *Client) SubscribeEvents() {
	c.SendCommand(map[string]interface{}{
		"type": "subscribe_events",
	})
}

func (c *Client) SubscribeEntities(entityIDs []string) {
	if len(entityIDs) == 0 {
		return
	}
	c.SendCommand(map[string]interface{}{
		"type":       "subscribe_entities",
		"entity_ids": entityIDs,
	})
}

func (c *Client) handleMessage(data []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("HA: JSON decode error: %v", err)
		return
	}

	msgType, _ := msg["type"].(string)

	switch msgType {
	case TypeAuthRequired:
		log.Println("HA: Auth required, sending token...")
		c.conn.WriteJSON(map[string]string{
			"type":         TypeAuth,
			"access_token": c.token,
		})
	case TypeAuthOK:
		log.Println("HA: Auth success!")
		c.isAuth = true
		c.SubscribeEvents()
		// Notify Savant we are connected
		c.onMessage(fmt.Sprintf("hass_websocket_connected,%s\n", time.Now().Format(time.RFC3339)))
	case TypeEvent:
		c.processEvent(msg)
	case TypeResult:
		// Handle command results if needed
	case TypePong:
		// Pong received
	default:
		// log.Printf("HA: Unknown message: %s", msgType)
	}
}

// processEvent handles incoming HA events and converts them to Savant format
func (c *Client) processEvent(msg map[string]interface{}) {
	event, ok := msg["event"].(map[string]interface{})
	if !ok {
		return
	}
	eventType, _ := event["event_type"].(string)

	if eventType == "state_changed" {
		data, ok := event["data"].(map[string]interface{})
		if !ok {
			return
		}
		newState, ok := data["new_state"].(map[string]interface{})
		if !ok || newState == nil {
			return
		}

		c.flattenAndSend(newState, []string{})
	} else if eventType == "call_service" {
		data, ok := event["data"].(map[string]interface{})
		if !ok {
			return
		}
		c.parseService(data)
	}
}

func (c *Client) parseService(data map[string]interface{}) {
	serviceData, ok := data["service_data"].(map[string]interface{})
	if !ok {
		return
	}

	// entity_id can be string or array of strings
	var entities []string
	switch v := serviceData["entity_id"].(type) {
	case string:
		entities = []string{v}
	case []interface{}:
		for _, e := range v {
			if s, ok := e.(string); ok {
				entities = append(entities, s)
			}
		}
	}

	if len(entities) == 0 {
		return
	}

	service, _ := data["service"].(string)
	domain, _ := data["domain"].(string)

	for _, entity := range entities {
		// "type:call_service,entity:#{entity},service:#{data['service']},domain:#{data['domain']}"
		msg := fmt.Sprintf("type:call_service,entity:%s,service:%s,domain:%s\n", entity, service, domain)
		c.onMessage(msg)
	}
}

// flattenAndSend recursively flattens the JSON and sends formatted strings
func (c *Client) flattenAndSend(data map[string]interface{}, parents []string) {
	entityID, _ := data["entity_id"].(string)
	
	// Handle State
	if state, ok := data["state"]; ok {
		c.sendSavantUpdate(entityID, parents, "state", state)
	}

	// Handle Attributes
	if attrs, ok := data["attributes"].(map[string]interface{}); ok {
		// Specific handling for 'attributes' key in path
		newParents := append(parents, "attributes")
		
		// Recursively handle attributes
		c.processMap(entityID, attrs, newParents)
	}
}

func (c *Client) processMap(entityID string, data map[string]interface{}, parents []string) {
	var mergedAttrs []string

	for k, v := range data {
		// Ruby: "#{k}:#{v}" - collects all keys regardless of filter
		mergedAttrs = append(mergedAttrs, fmt.Sprintf("%s:%v", k, v))

		if !c.includedWithFilter(k) {
			continue
		}
		switch val := v.(type) {
		case map[string]interface{}:
			c.processMap(entityID, val, append(parents, k))
		case []interface{}:
			strs := make([]string, len(val))
			for i, item := range val {
				strs[i] = fmt.Sprintf("%v", item)
			}
			c.sendSavantUpdate(entityID, append(parents, k), k, strings.Join(strs, ","))
		default:
			c.sendSavantUpdate(entityID, append(parents, k), k, val)
		}
	}

	// Also store a merged attributes string
	if c.includedWithFilter("attributes") {
		// Ruby: update_hass_data(parent_key, parents + ['attributes'], parent_key, merged_attrs)
		// Here entityID is the parent_key passed down?
		// At top level call: processMap(entityID, attrs, ["attributes"])
		// So parents is ["attributes"].
		// We want to append "attributes" again?
		// Ruby: parents + ['attributes']
		// But in Go recursion we already appended keys.
		
		// Let's look at call site:
		// c.processMap(entityID, attrs, newParents) where newParents = parents + "attributes"
		
		// So we are already in the "attributes" branch.
		// Ruby code:
		// update_with_hash(eid, atr, parents + ['attributes'])
		//   -> update_hass_data(eid, parents + ['attributes'] + ['attributes'], eid, merged)
		// Wait, Ruby adds 'attributes' inside update_with_hash?
		// L223: parents + ['attributes']
		// So it adds ANOTHER 'attributes' level?
		// Yes.
		
		c.sendSavantUpdate(entityID, append(parents, "attributes"), entityID, strings.Join(mergedAttrs, ","))
	}
}

func (c *Client) sendSavantUpdate(entityID string, parents []string, attrName string, value interface{}) {
	if value == nil || !c.includedWithFilter(attrName) {
		return
	}
	
	// Hack for brightness (from Ruby code)
	// value = 3 if attr_name == 'brightness' && [1, 2].include?(value)
	if attrName == "brightness" {
		if vInt, ok := value.(float64); ok { // JSON numbers are floats
			if vInt == 1 || vInt == 2 {
				value = 3
			}
		}
	}

	joinedParents := strings.Join(parents, "_")
	
	// Format: entity_id=...&substitute_id=...&parent_keys=...&attr_name=...&attr_value=...
	subID := c.getSubstituteID(entityID)
	
	output := fmt.Sprintf("entity_id=%s&substitute_id=%s&parent_keys=%s&attr_name=%s&attr_value=%v\n",
		entityID, subID, joinedParents, attrName, value)
	
	c.onMessage(output)
}
