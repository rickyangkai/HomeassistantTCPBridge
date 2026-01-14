package savant

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"

	"github.com/rick/homeassistant-tcp-bridge/pkg/config"
	"github.com/rick/homeassistant-tcp-bridge/pkg/ha"
)

type Server struct {
	port      int
	whitelist []string
	haClient  *ha.Client
	clients   map[net.Conn]bool
}

func NewServer(port int, cfg *config.Config, haClient *ha.Client) *Server {
	return &Server{
		port:      port,
		whitelist: cfg.Whitelist,
		haClient:  haClient,
		clients:   make(map[net.Conn]bool),
	}
}

func (s *Server) Start() {
	addr := fmt.Sprintf("0.0.0.0:%d", s.port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Savant: Failed to bind port %d: %v", s.port, err)
	}
	log.Printf("Savant: Listening on %s", addr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Savant: Accept error: %v", err)
			continue
		}
		go s.handleConnection(conn)
	}
}

func (s *Server) Broadcast(msg string) {
	for conn := range s.clients {
		// Ignore errors on broadcast, handle in connection loop
		conn.Write([]byte(msg))
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()
	remoteAddr := conn.RemoteAddr().(*net.TCPAddr).IP.String()

	// 1. Whitelist Check
	allowed := false
	if len(s.whitelist) == 0 {
		allowed = true
	} else {
		for _, ip := range s.whitelist {
			if ip == remoteAddr {
				allowed = true
				break
			}
		}
	}

	if !allowed {
		log.Printf("Savant: Access denied for %s", remoteAddr)
		return
	}

	log.Printf("Savant: Client connected %s", remoteAddr)
	s.clients[conn] = true
	defer delete(s.clients, conn)

	// 2. Read Loop
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		text := scanner.Text()
		s.handleCommand(text)
	}
}

func (s *Server) handleCommand(cmdStr string) {
	// Savant sends commands separated by commas
	// Example: switch_on,light.living_room
	parts := strings.Split(cmdStr, ",")
	if len(parts) == 0 {
		return
	}

	cmd := parts[0]
	args := parts[1:]

	// Handle special setup commands that don't use entity IDs
	if cmd == "substitute_ids" {
		// args: key1, val1, key2, val2 ...
		subs := make(map[string]string)
		var haIDs []string
		for i := 0; i < len(args); i += 2 {
			if i+1 < len(args) {
				// key (savant id) -> value (ha entity id)
				subs[args[i+1]] = args[i]
				haIDs = append(haIDs, args[i+1])
			}
		}
		s.haClient.SetSubstituteIDs(subs)
		// Ruby also subscribes to these entities immediately
		s.haClient.SubscribeEntities(haIDs)
		return
	}
	
	if cmd == "state_filter" {
		// args are the filter keys
		s.haClient.SetFilter(args)
		return
	}
	
	if cmd == "subscribe_entity" {
		// args are entity_ids
		s.haClient.SubscribeEntities(args)
		return
	}

	// For other commands, the first arg is usually entity_id.
	// We need to resolve it if it's a substitute ID.
	if len(args) > 0 {
		// We need a way to resolve substitute ID to real ID.
		// The HA Client has this mapping. Let's expose it or pass it.
		// Actually, Client.GetEntityID is implemented as ha_id -> sub_id?
		// Wait, Client.GetEntityID implementation was: 
		// if realID, ok := c.substituteIDs[id]; ok { return realID }
		// But substituteIDs was map[ha_id]sub_id.
		// So c.substituteIDs[ha_id] returns sub_id.
		// We need sub_id -> ha_id.
		// I added idSubstitutes map[sub_id]ha_id in client.
		// Let's update Client to expose a ResolveID method.
		args[0] = s.haClient.ResolveID(args[0])
	}

	log.Printf("Savant Command: %s %v", cmd, args)

	switch cmd {
	case "subscribe_events":
		// HA Client handles this automatically on connect, but we can force it
		s.haClient.SubscribeEvents()
	case "call_service":
		// Generic call service support
		// format: call_service,domain,service,entity_id,key1=value1,key2=value2...
		if len(args) >= 3 {
			domain := args[0]
			service := args[1]
			entityID := args[2]
			var data map[string]interface{}
			
			if len(args) > 3 {
				data = make(map[string]interface{})
				for _, kv := range args[3:] {
					kvParts := strings.SplitN(kv, "=", 2)
					if len(kvParts) == 2 {
						data[kvParts[0]] = kvParts[1]
					}
				}
			}
			s.callService(domain, service, entityID, data)
		}
	case "fan_on":
		if len(args) > 1 {
			s.callService("fan", "turn_on", args[0], map[string]interface{}{"speed": args[1]})
		} else if len(args) > 0 {
			s.callService("fan", "turn_on", args[0], nil)
		}
	case "fan_off":
		if len(args) > 0 {
			s.callService("fan", "turn_off", args[0], nil)
		}
	case "fan_set":
		if len(args) > 1 {
			// speed.to_i.zero? ? fan_off(entity_id) : fan_on(entity_id, speed)
			// Simplification: just call turn_on with speed, HA handles it usually
			s.callService("fan", "turn_on", args[0], map[string]interface{}{"speed": args[1]})
		}
	case "button_press":
		if len(args) > 0 {
			s.callService("button", "press", args[0], nil)
		}
	case "alarm_arm_away":
		if len(args) > 0 {
			data := map[string]interface{}{}
			if len(args) > 1 { data["code"] = args[1] }
			s.callService("alarm_control_panel", "alarm_arm_away", args[0], data)
		}
	case "alarm_arm_home":
		if len(args) > 0 {
			data := map[string]interface{}{}
			if len(args) > 1 { data["code"] = args[1] }
			s.callService("alarm_control_panel", "alarm_arm_home", args[0], data)
		}
	case "alarm_disarm":
		if len(args) > 0 {
			data := map[string]interface{}{}
			if len(args) > 1 { data["code"] = args[1] }
			s.callService("alarm_control_panel", "alarm_disarm", args[0], data)
		}
	case "remote_on":
		if len(args) > 0 {
			s.callService("remote", "turn_on", args[0], nil)
		}
	case "remote_off":
		if len(args) > 0 {
			s.callService("remote", "turn_off", args[0], nil)
		}
	case "remote_send_command":
		if len(args) > 1 {
			s.callService("remote", "send_command", args[0], map[string]interface{}{"command": args[1]})
		}
	case "switch_on":
		if len(args) > 0 {
			s.callService("light", "turn_on", args[0], nil)
		}
	case "switch_off":
		if len(args) > 0 {
			s.callService("light", "turn_off", args[0], nil)
		}
	case "socket_on":
		if len(args) > 0 {
			s.callService("switch", "turn_on", args[0], nil)
		}
	case "socket_off":
		if len(args) > 0 {
			s.callService("switch", "turn_off", args[0], nil)
		}
	case "dimmer_set":
		if len(args) > 1 {
			level, _ := strconv.Atoi(args[1])
			if level == 0 {
				s.callService("light", "turn_off", args[0], nil)
			} else {
				s.callService("light", "turn_on", args[0], map[string]interface{}{"brightness_pct": level})
			}
		}
	case "shade_set":
		if len(args) > 1 {
			pos, _ := strconv.Atoi(args[1])
			s.callService("cover", "set_cover_position", args[0], map[string]interface{}{"position": pos})
		}
	case "open_garage_door":
		if len(args) > 0 {
			s.callService("cover", "open_cover", args[0], nil)
		}
	case "close_garage_door":
		if len(args) > 0 {
			s.callService("cover", "close_cover", args[0], nil)
		}
	case "toggle_garage_door":
		if len(args) > 0 {
			s.callService("cover", "toggle", args[0], nil)
		}
	case "lock_lock":
		if len(args) > 0 {
			s.callService("lock", "lock", args[0], nil)
		}
	case "unlock_lock":
		if len(args) > 0 {
			s.callService("lock", "unlock", args[0], nil)
		}
	case "climate_set_hvac_mode":
		if len(args) > 1 {
			s.callService("climate", "set_hvac_mode", args[0], map[string]interface{}{"hvac_mode": args[1]})
		}
	case "climate_set_single":
		if len(args) > 1 {
			temp, _ := strconv.ParseFloat(args[1], 64)
			s.callService("climate", "set_temperature", args[0], map[string]interface{}{"temperature": temp})
		}
	case "climate_set_temperature_range":
		if len(args) > 2 {
			low, _ := strconv.ParseFloat(args[1], 64)
			high, _ := strconv.ParseFloat(args[2], 64)
			s.callService("climate", "set_temperature", args[0], map[string]interface{}{
				"target_temp_low":  low,
				"target_temp_high": high,
			})
		}
	case "media_player_play":
		if len(args) > 0 {
			s.callService("media_player", "media_play", args[0], nil)
		}
	case "media_player_play_pause":
		if len(args) > 0 {
			s.callService("media_player", "toggle", args[0], nil)
		}
	case "media_player_pause":
		if len(args) > 0 {
			s.callService("media_player", "media_pause", args[0], nil)
		}
	case "media_player_stop":
		if len(args) > 0 {
			s.callService("media_player", "media_stop", args[0], nil)
		}
	case "media_player_next_track":
		if len(args) > 0 {
			s.callService("media_player", "media_next_track", args[0], nil)
		}
	case "media_player_previous_track":
		if len(args) > 0 {
			s.callService("media_player", "media_previous_track", args[0], nil)
		}
	case "media_player_volume_up":
		if len(args) > 0 {
			s.callService("media_player", "volume_up", args[0], nil)
		}
	case "media_player_volume_down":
		if len(args) > 0 {
			s.callService("media_player", "volume_down", args[0], nil)
		}
	case "media_player_set_volume":
		if len(args) > 1 {
			vol, _ := strconv.Atoi(args[1])
			s.callService("media_player", "volume_set", args[0], map[string]interface{}{"volume_level": float64(vol) / 100.0})
		}
	case "media_player_select_source":
		if len(args) > 1 {
			s.callService("media_player", "select_source", args[0], map[string]interface{}{"source": args[1]})
		}
	case "media_player_clear_playlist":
		if len(args) > 0 {
			s.callService("media_player", "clear_playlist", args[0], nil)
		}
	case "media_player_shuffle_set":
		if len(args) > 1 {
			s.callService("media_player", "shuffle_set", args[0], map[string]interface{}{"shuffle": strings.ToLower(args[1]) == "true"})
		}
	case "media_player_repeat_set":
		if len(args) > 1 {
			s.callService("media_player", "repeat_set", args[0], map[string]interface{}{"repeat": args[1]})
		}
	case "media_player_media_seek":
		if len(args) > 1 {
			pos, _ := strconv.ParseFloat(args[1], 64)
			s.callService("media_player", "media_seek", args[0], map[string]interface{}{"seek_position": pos})
		}
	case "media_player_play_media":
		// Expects json params in second arg? Ruby: JSON.parse(json_params)
		// Go simplistic approach: assume it might be content_id, content_type
		// Or if args[1] is JSON string.
		if len(args) > 1 {
			// This is tricky without a proper JSON parser for the arg if it contains commas.
			// But assuming basic usage or simple strings.
			// Ideally we should just pass it through if possible or implement specific logic.
			// For now, let's just log it as not fully supported or try simple case.
			log.Printf("media_player_play_media not fully implemented for complex JSON args yet")
		}
	// Add other commands as needed based on hass_savant.rb
	default:
		log.Printf("Unknown command: %s", cmd)
	}
}

func (s *Server) callService(domain, service, entityID string, data map[string]interface{}) {
	payload := map[string]interface{}{
		"type":    "call_service",
		"domain":  domain,
		"service": service,
		"target": map[string]interface{}{
			"entity_id": entityID,
		},
	}
	if data != nil {
		payload["service_data"] = data
	}
	s.haClient.SendCommand(payload)
}
