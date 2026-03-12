package zooid

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	"fiatjaf.com/nostr"
	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/webhook"
	"slices"
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
		log.Printf("[livekit webhook] 404: livekit not configured")
		http.NotFound(w, r)
		return
	}

	kp := auth.NewSimpleKeyProvider(cfg.APIKey, cfg.APISecret)
	event, err := webhook.ReceiveWebhookEvent(r, kp)
	if err != nil {
		log.Printf("[livekit webhook] invalid webhook: %v", err)
		http.Error(w, "invalid webhook: "+err.Error(), http.StatusUnauthorized)
		return
	}

	log.Printf("[livekit webhook] received event: %s", event.Event)

	room := event.GetRoom()
	if room == nil {
		log.Printf("[livekit webhook] missing room in event")
		http.Error(w, "missing room", http.StatusBadRequest)
		return
	}
	groupId := room.GetName()
	if groupId == "" {
		log.Printf("[livekit webhook] missing room name")
		http.Error(w, "missing room name", http.StatusBadRequest)
		return
	}

	meta, found := instance.Groups.GetMetadata(groupId)
	if !found {
		log.Printf("[livekit webhook] group not found: %s", groupId)
		http.NotFound(w, r)
		return
	}

	if !HasTag(meta.Tags, "livekit") {
		log.Printf("[livekit webhook] livekit not enabled for group: %s", groupId)
		http.Error(w, "livekit not enabled for this group", http.StatusForbidden)
		return
	}

	switch event.Event {
	case webhook.EventParticipantJoined, webhook.EventParticipantLeft:
		participant := event.GetParticipant()
		if participant == nil {
			log.Printf("[livekit webhook] missing participant in %s event for room %s", event.Event, groupId)
			http.Error(w, "missing participant", http.StatusBadRequest)
			return
		}
		if len(participant.Identity) < 64 {
			log.Printf("[livekit webhook] participant identity too short (%d chars) in %s for room %s: %q",
				len(participant.Identity), event.Event, groupId, participant.Identity)
			http.Error(w, "missing participant", http.StatusBadRequest)
			return
		}

		identityPrefix := participant.Identity[0:64]
		pubkey, err := nostr.PubKeyFromHex(identityPrefix)
		if err != nil {
			log.Printf("[livekit webhook] invalid nostr pubkey in identity %q: %v", participant.Identity, err)
			w.WriteHeader(http.StatusNoContent)
			return
		}

		connected := event.Event == webhook.EventParticipantJoined
		log.Printf("[livekit webhook] %s room=%s pubkey=%s connected=%v",
			event.Event, groupId, pubkey.Hex()[:16]+"...", connected)

		if err := instance.updateLiveKitPresence(groupId, pubkey, connected); err != nil {
			log.Printf("[livekit webhook] failed to update presence: %v", err)
			http.Error(w, "failed to update livekit participants: "+err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("[livekit webhook] successfully updated presence for room %s", groupId)
	default:
		log.Printf("[livekit webhook] ignoring unhandled event type: %s for room %s", event.Event, groupId)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (instance *Instance) updateLiveKitPresence(groupId string, pubkey nostr.PubKey, connected bool) error {
	participants := instance.getLiveKitParticipants(groupId)
	beforeCount := len(participants)

	if connected {
		if !slices.Contains(participants, pubkey) {
			participants = append(participants, pubkey)
		}
	} else {
		if idx := slices.Index(participants, pubkey); idx != -1 {
			participants[idx] = participants[len(participants)-1]
			participants = participants[:len(participants)-1]
		} else {
			log.Printf("[livekit webhook] participant %s not in list when processing leave (had %d participants)",
				pubkey.Hex()[:16]+"...", beforeCount)
		}
	}

	log.Printf("[livekit webhook] presence update: room=%s connected=%v before=%d after=%d",
		groupId, connected, beforeCount, len(participants))

	return instance.publishLiveKitPresence(groupId, participants)
}

func (instance *Instance) getLiveKitParticipants(groupId string) []nostr.PubKey {
	filter := nostr.Filter{
		Kinds:   []nostr.Kind{nostr.KindSimpleGroupLiveKitParticipants},
		Authors: []nostr.PubKey{instance.Config.GetSelf()},
		Tags:    nostr.TagMap{"d": []string{groupId}},
	}

	for event := range instance.Events.QueryEvents(filter, 1) {
		var participants []nostr.PubKey
		for tag := range event.Tags.FindAll("p") {
			if pk, err := nostr.PubKeyFromHex(tag[1]); err == nil {
				participants = append(participants, pk)
			}
		}
		log.Printf("[livekit webhook] loaded %d participants from existing 39004 for room %s", len(participants), groupId)
		return participants
	}
	log.Printf("[livekit webhook] no existing 39004 event for room %s, starting with empty list", groupId)
	return nil
}

func (instance *Instance) publishLiveKitPresence(groupId string, participants []nostr.PubKey) error {
	tags := nostr.Tags{nostr.Tag{"d", groupId}}
	for _, pk := range participants {
		tags = append(tags, nostr.Tag{"p", pk.Hex()})
	}

	event := nostr.Event{
		Kind:      nostr.KindSimpleGroupLiveKitParticipants,
		CreatedAt: nostr.Now(),
		Tags:      tags,
	}

	return instance.Events.SignAndStoreEvent(&event, true)
}
