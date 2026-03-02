package zooid

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"fiatjaf.com/nostr"
	"github.com/livekit/protocol/auth"
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
	at.SetIdentity(pubkey.Hex())

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
	livekitRoomsMu.RLock()
	if livekitRooms[roomName] {
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
		livekitRooms[roomName] = true
		livekitRoomsMu.Unlock()
		return nil
	}

	return fmt.Errorf("failed to create room: %s", resp.Status)
}

func (instance *Instance) livekitTokenHandler(w http.ResponseWriter, r *http.Request) {
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

	meta, found := instance.Groups.GetMetadata(groupId)
	if !found {
		http.Error(w, "group not found", http.StatusNotFound)
		return
	}

	if !HasTag(meta.Tags, "livekit") {
		http.Error(w, "livekit not enabled for this group", http.StatusForbidden)
		return
	}

	if !instance.Groups.HasAccess(groupId, pubkey) {
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
