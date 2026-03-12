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
	case webhook.EventParticipantJoined, webhook.EventParticipantLeft:
		participant := event.GetParticipant()
		if participant == nil || len(participant.Identity) < 64 {
			http.Error(w, "missing participant", http.StatusBadRequest)
			return
		}

		if _, err := nostr.PubKeyFromHex(participant.Identity[0:64]); err != nil {
			log.Printf("[livekit webhook] invalid nostr pubkey in identity: %v", err)
			w.WriteHeader(http.StatusNoContent)
			return
		}

		connected := event.Event == webhook.EventParticipantJoined
		if err := instance.updateLiveKitPresence(groupId, participant.Identity, connected); err != nil {
			http.Error(w, "failed to update livekit participants: "+err.Error(), http.StatusInternalServerError)
			return
		}
	default:
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (instance *Instance) updateLiveKitPresence(groupId string, identity string, connected bool) error {
	identities := instance.getLiveKitParticipantIdentities(groupId)

	if connected {
		if !slices.Contains(identities, identity) {
			identities = append(identities, identity)
		}
	} else {
		if idx := slices.Index(identities, identity); idx != -1 {
			identities[idx] = identities[len(identities)-1]
			identities = identities[:len(identities)-1]
		} else {
			log.Printf("[livekit webhook] identity %q not in list when processing leave (had %d participants)",
				identity, len(identities))
		}
	}

	log.Printf("[livekit webhook] presence update: room=%s connected=%v count=%d",
		groupId, connected, len(identities))

	return instance.publishLiveKitPresence(groupId, identities)
}

func (instance *Instance) getLiveKitParticipantIdentities(groupId string) []string {
	filter := nostr.Filter{
		Kinds:   []nostr.Kind{nostr.KindSimpleGroupLiveKitParticipants},
		Authors: []nostr.PubKey{instance.Config.GetSelf()},
		Tags:    nostr.TagMap{"d": []string{groupId}},
	}

	for event := range instance.Events.QueryEvents(filter, 1) {
		var identities []string
		for tag := range event.Tags.FindAll("participant") {
			if len(tag) >= 2 && tag[1] != "" {
				if !slices.Contains(identities, tag[1]) {
					identities = append(identities, tag[1])
				}
			}
		}
		return identities
	}
	return nil
}

func (instance *Instance) publishLiveKitPresence(groupId string, identities []string) error {
	tags := nostr.Tags{nostr.Tag{"d", groupId}}
	for _, identity := range identities {
		tags = append(tags, nostr.Tag{"participant", identity})
	}

	event := nostr.Event{
		Kind:      nostr.KindSimpleGroupLiveKitParticipants,
		CreatedAt: nostr.Now(),
		Tags:      tags,
	}

	return instance.Events.SignAndStoreEvent(&event, true)
}
