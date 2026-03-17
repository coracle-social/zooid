package zooid

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"

	"fiatjaf.com/nostr"
	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/webhook"
)

var (
	livekitHTTPClient = &http.Client{}
	livekitRooms      = make(map[string]bool)
	livekitRoomsMu    sync.RWMutex
)

type TokenEndpointResponse struct {
	ServerURL        string `json:"server_url"`
	ParticipantToken string `json:"participant_token"`
}

func generateLivekitToken(apiKey, apiSecret, room string, pubkey nostr.PubKey) string {
	at := auth.NewAccessToken(apiKey, apiSecret)
	at.SetVideoGrant(&auth.VideoGrant{
		RoomJoin: true,
		Room:     room,
	})
	at.SetIdentity(pubkey.Hex() + ":" + RandomString(16))

	jwt, _ := at.ToJWT()
	return jwt
}

func generateLivekitServerToken(apiKey, apiSecret string) string {
	at := auth.NewAccessToken(apiKey, apiSecret)
	at.SetVideoGrant(&auth.VideoGrant{
		RoomCreate: true,
		RoomList:   true,
		RoomAdmin:  true,
	})

	jwt, _ := at.ToJWT()
	return jwt
}

func ensureLivekitRoom(apiKey, apiSecret, serverURL, roomName string) error {
	roomKey := serverURL + "'" + roomName

	livekitRoomsMu.RLock()
	if livekitRooms[roomKey] {
		livekitRoomsMu.RUnlock()
		return nil
	}
	livekitRoomsMu.RUnlock()

	httpURL := strings.Replace(strings.Replace(serverURL, "wss://", "https://", 1), "ws://", "http://", 1)
	url := fmt.Sprintf("%s/twirp/livekit.RoomService/CreateRoom", httpURL)

	reqBody, _ := json.Marshal(map[string]interface{}{
		"name": roomName,
	})

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+generateLivekitServerToken(apiKey, apiSecret))

	resp, err := livekitHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusConflict {
		livekitRoomsMu.Lock()
		livekitRooms[roomKey] = true
		livekitRoomsMu.Unlock()
		return nil
	}

	return fmt.Errorf("failed to create room: %s", resp.Status)
}

func fetchLivekitParticipants(apiKey, apiSecret, serverURL, roomName string) ([]nostr.PubKey, error) {
	httpURL := strings.Replace(strings.Replace(serverURL, "wss://", "https://", 1), "ws://", "http://", 1)
	url := fmt.Sprintf("%s/twirp/livekit.RoomService/ListParticipants", httpURL)

	reqBody, _ := json.Marshal(map[string]interface{}{
		"room": roomName,
	})

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+generateLivekitServerToken(apiKey, apiSecret))

	resp, err := livekitHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ListParticipants failed: %s %s", resp.Status, string(body))
	}

	var result struct {
		Participants []struct {
			Identity string `json:"identity"`
		} `json:"participants"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode ListParticipants response: %w", err)
	}

	seen := make(map[nostr.PubKey]struct{})
	var pubkeys []nostr.PubKey
	for _, p := range result.Participants {
		hexPart, _, _ := strings.Cut(p.Identity, ":")
		pk, err := nostr.PubKeyFromHex(hexPart)
		if err != nil {
			log.Printf("[livekit] dropping participant with unparseable identity %q: %v", p.Identity, err)
			continue
		}
		if _, dup := seen[pk]; !dup {
			seen[pk] = struct{}{}
			pubkeys = append(pubkeys, pk)
		}
	}

	return pubkeys, nil
}

func (instance *Instance) livekitSupportHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")

	if r.Method == http.MethodOptions {
		return
	}

	cfg := instance.Config.Livekit
	if cfg.APIKey == "" {
		http.NotFound(w, r)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (instance *Instance) livekitTokenHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")

	if r.Method == http.MethodOptions {
		return
	}

	cfg := instance.Config.Livekit
	if cfg.APIKey == "" {
		http.NotFound(w, r)
		return
	}

	groupId := r.PathValue("groupId")

	pubkey, err := validateNIP98Auth(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if !instance.Management.IsMember(pubkey) {
		http.Error(w, "not a member of this relay", http.StatusForbidden)
		return
	}

	meta, found := instance.Groups.GetMetadata(groupId)
	if !found {
		http.Error(w, "group not found", http.StatusNotFound)
		return
	}

	if !HasTag(meta.Tags, "livekit") {
		http.Error(w, "livekit not enabled for this group", http.StatusForbidden)
		return
	}

	if HasTag(meta.Tags, "restricted") && !instance.Groups.HasAccess(groupId, pubkey) {
		http.Error(w, "not a group member", http.StatusForbidden)
		return
	}

	if err := ensureLivekitRoom(cfg.APIKey, cfg.APISecret, cfg.ServerURL, groupId); err != nil {
		http.Error(w, "failed to create room", http.StatusInternalServerError)
		return
	}

	token := generateLivekitToken(cfg.APIKey, cfg.APISecret, groupId, pubkey)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TokenEndpointResponse{
		ServerURL:        cfg.ServerURL,
		ParticipantToken: token,
	})
}

func (instance *Instance) livekitWebhookHandler(w http.ResponseWriter, r *http.Request) {
	cfg := instance.Config.Livekit
	if cfg.APIKey == "" || cfg.APISecret == "" {
		http.NotFound(w, r)
		return
	}

	kp := auth.NewSimpleKeyProvider(cfg.APIKey, cfg.APISecret)
	event, err := webhook.ReceiveWebhookEvent(r, kp)
	if err != nil {
		http.Error(w, "invalid webhook: "+err.Error(), http.StatusUnauthorized)
		return
	}

	room := event.GetRoom()
	if room == nil {
		http.Error(w, "missing room", http.StatusBadRequest)
		return
	}
	groupId := room.GetName()
	if groupId == "" {
		http.Error(w, "missing room name", http.StatusBadRequest)
		return
	}

	meta, found := instance.Groups.GetMetadata(groupId)
	if !found {
		http.NotFound(w, r)
		return
	}

	if !HasTag(meta.Tags, "livekit") {
		http.Error(w, "livekit not enabled for this group", http.StatusForbidden)
		return
	}

	switch event.Event {
	case webhook.EventRoomFinished:
		if err := instance.publishLiveKitPresence(groupId, nil); err != nil {
			http.Error(w, "failed to update livekit participants: "+err.Error(), http.StatusInternalServerError)
			return
		}
	case webhook.EventParticipantJoined, webhook.EventParticipantLeft, webhook.EventParticipantConnectionAborted:
		pubkeys, err := fetchLivekitParticipants(cfg.APIKey, cfg.APISecret, cfg.ServerURL, groupId)
		if err != nil {
			log.Printf("[livekit webhook] failed to fetch participants: %v", err)
			http.Error(w, "failed to fetch livekit participants: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if err := instance.publishLiveKitPresence(groupId, pubkeys); err != nil {
			http.Error(w, "failed to update livekit participants: "+err.Error(), http.StatusInternalServerError)
			return
		}
	default:
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (instance *Instance) publishLiveKitPresence(groupId string, pubkeys []nostr.PubKey) error {
	tags := nostr.Tags{nostr.Tag{"d", groupId}}
	for _, pk := range pubkeys {
		tags = append(tags, nostr.Tag{"participant", pk.Hex()})
	}

	event := nostr.Event{
		Kind:      nostr.KindSimpleGroupLiveKitParticipants,
		CreatedAt: nostr.Now(),
		Tags:      tags,
	}

	return instance.Events.SignAndStoreEvent(&event, true)
}
