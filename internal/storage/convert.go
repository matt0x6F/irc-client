package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"

	db "github.com/matt0x6f/irc-client/internal/storage/generated"
)

// Conversion helpers between SQLC generated types (db package) and our application types

func convertNetworkFromDB(n db.Network) Network {
	result := Network{
		ID:          n.ID,
		Name:        n.Name,
		Address:     n.Address,
		Port:        int(n.Port), // Convert int64 to int
		TLS:         n.Tls,
		Nickname:    n.Nickname,
		Username:    n.Username,
		Realname:    n.Realname,
		Password:    convertNullString(n.Password),
		SASLEnabled: n.SaslEnabled,
		AutoConnect: n.AutoConnect,
		CreatedAt:   n.CreatedAt,
		UpdatedAt:   n.UpdatedAt,
	}
	if n.SaslMechanism.Valid {
		result.SASLMechanism = &n.SaslMechanism.String
	}
	if n.SaslUsername.Valid {
		result.SASLUsername = &n.SaslUsername.String
	}
	if n.SaslPassword.Valid {
		result.SASLPassword = &n.SaslPassword.String
	}
	if n.SaslExternalCert.Valid {
		result.SASLExternalCert = &n.SaslExternalCert.String
	}
	return result
}

func convertNetworkToDBCreateParams(n *Network) db.CreateNetworkParams {
	params := db.CreateNetworkParams{
		Name:        n.Name,
		Address:     n.Address,
		Port:        int64(n.Port), // Convert int to int64
		Tls:         n.TLS,
		Nickname:    n.Nickname,
		Username:    n.Username,
		Realname:    n.Realname,
		Password:    convertToNullString(n.Password),
		SaslEnabled: n.SASLEnabled,
		AutoConnect: n.AutoConnect,
		CreatedAt:   n.CreatedAt,
		UpdatedAt:   n.UpdatedAt,
	}
	if n.SASLMechanism != nil {
		params.SaslMechanism = sql.NullString{String: *n.SASLMechanism, Valid: true}
	}
	if n.SASLUsername != nil {
		params.SaslUsername = sql.NullString{String: *n.SASLUsername, Valid: true}
	}
	if n.SASLPassword != nil {
		params.SaslPassword = sql.NullString{String: *n.SASLPassword, Valid: true}
	}
	if n.SASLExternalCert != nil {
		params.SaslExternalCert = sql.NullString{String: *n.SASLExternalCert, Valid: true}
	}
	return params
}

func convertNetworkToDBUpdateParams(n *Network) db.UpdateNetworkParams {
	params := db.UpdateNetworkParams{
		Name:        n.Name,
		Address:     n.Address,
		Port:        int64(n.Port),
		Tls:         n.TLS,
		Nickname:    n.Nickname,
		Username:    n.Username,
		Realname:    n.Realname,
		Password:    convertToNullString(n.Password),
		SaslEnabled: n.SASLEnabled,
		AutoConnect: n.AutoConnect,
		UpdatedAt:   n.UpdatedAt,
		ID:          n.ID,
	}
	if n.SASLMechanism != nil {
		params.SaslMechanism = sql.NullString{String: *n.SASLMechanism, Valid: true}
	}
	if n.SASLUsername != nil {
		params.SaslUsername = sql.NullString{String: *n.SASLUsername, Valid: true}
	}
	if n.SASLPassword != nil {
		params.SaslPassword = sql.NullString{String: *n.SASLPassword, Valid: true}
	}
	if n.SASLExternalCert != nil {
		params.SaslExternalCert = sql.NullString{String: *n.SASLExternalCert, Valid: true}
	}
	return params
}

func convertServerFromDB(s db.Server) Server {
	return Server{
		ID:        s.ID,
		NetworkID: s.NetworkID,
		Address:   s.Address,
		Port:      int(s.Port),
		TLS:       s.Tls,
		Order:     int(s.Order),
		CreatedAt: s.CreatedAt,
	}
}

func convertServerToDBCreateParams(s *Server) db.CreateServerParams {
	return db.CreateServerParams{
		NetworkID: s.NetworkID,
		Address:   s.Address,
		Port:      int64(s.Port),
		Tls:       s.TLS,
		Order:     int64(s.Order),
		CreatedAt: s.CreatedAt,
	}
}

func convertServerToDBUpdateParams(s *Server) db.UpdateServerParams {
	return db.UpdateServerParams{
		Address: s.Address,
		Port:    int64(s.Port),
		Tls:     s.TLS,
		Order:   int64(s.Order),
		ID:      s.ID,
	}
}

func convertChannelFromDB(c db.Channel) Channel {
	result := Channel{
		ID:        c.ID,
		NetworkID: c.NetworkID,
		Name:      c.Name,
		Topic:     convertNullString(c.Topic),
		Modes:     convertNullString(c.Modes),
		AutoJoin:  c.AutoJoin,
		IsOpen:    c.IsOpen,
		CreatedAt: c.CreatedAt,
	}
	if c.UpdatedAt.Valid {
		result.UpdatedAt = &c.UpdatedAt.Time
	}
	return result
}

func convertChannelToDBCreateParams(c *Channel) db.CreateChannelParams {
	return db.CreateChannelParams{
		NetworkID: c.NetworkID,
		Name:      c.Name,
		AutoJoin:  c.AutoJoin,
		IsOpen:    c.IsOpen,
		CreatedAt: c.CreatedAt,
	}
}

func convertChannelUserFromDB(cu db.ChannelUser) ChannelUser {
	return ChannelUser{
		ID:        cu.ID,
		ChannelID: cu.ChannelID,
		Nickname:  cu.Nickname,
		Modes:     convertNullString(cu.Modes),
		CreatedAt: cu.CreatedAt,
		UpdatedAt: cu.UpdatedAt,
	}
}

func convertMessageFromDB(m db.Message) Message {
	result := Message{
		ID:          m.ID,
		NetworkID:   m.NetworkID,
		User:        m.User,
		Message:     m.Message,
		MessageType: m.MessageType,
		Timestamp:   m.Timestamp,
		RawLine:     convertNullString(m.RawLine),
	}
	if m.ChannelID.Valid {
		result.ChannelID = &m.ChannelID.Int64
	}
	return result
}

func convertMessageToDBCreateParams(m Message) db.CreateMessageParams {
	var channelID sql.NullInt64
	if m.ChannelID != nil {
		channelID = sql.NullInt64{Int64: *m.ChannelID, Valid: true}
	}
	return db.CreateMessageParams{
		NetworkID:   m.NetworkID,
		ChannelID:   channelID,
		User:        m.User,
		Message:     m.Message,
		MessageType: m.MessageType,
		Timestamp:   m.Timestamp,
		RawLine:     convertToNullString(m.RawLine),
	}
}

func convertPMConversationFromDB(pmc db.PrivateMessageConversation) PrivateMessageConversation {
	result := PrivateMessageConversation{
		ID:         pmc.ID,
		NetworkID:  pmc.NetworkID,
		TargetUser: pmc.TargetUser,
		IsOpen:     pmc.IsOpen,
		CreatedAt:  pmc.CreatedAt,
	}
	if pmc.UpdatedAt.Valid {
		result.UpdatedAt = &pmc.UpdatedAt.Time
	}
	return result
}

func convertPMConversationToDBCreateParams(pmc *PrivateMessageConversation) db.CreatePMConversationParams {
	var updatedAt sql.NullTime
	if pmc.UpdatedAt != nil {
		updatedAt = sql.NullTime{Time: *pmc.UpdatedAt, Valid: true}
	}
	return db.CreatePMConversationParams{
		NetworkID:  pmc.NetworkID,
		TargetUser: pmc.TargetUser,
		IsOpen:     pmc.IsOpen,
		CreatedAt:  pmc.CreatedAt,
		UpdatedAt:  updatedAt,
	}
}

func convertPluginConfigFromDB(pc db.PluginConfig) (*PluginConfig, error) {
	config := PluginConfig{
		Name:      pc.Name,
		Enabled:   pc.Enabled,
		CreatedAt: pc.CreatedAt,
		UpdatedAt: pc.UpdatedAt,
	}

	// Decode JSON config
	if len(pc.Config) > 0 {
		if err := json.Unmarshal(pc.Config, &config.Config); err != nil {
			return nil, fmt.Errorf("failed to decode plugin config JSON: %w", err)
		}
	} else {
		config.Config = make(map[string]interface{})
	}

	// Decode JSON config_schema
	if len(pc.ConfigSchema) > 0 {
		if err := json.Unmarshal(pc.ConfigSchema, &config.ConfigSchema); err != nil {
			return nil, fmt.Errorf("failed to decode plugin config_schema JSON: %w", err)
		}
	} else {
		config.ConfigSchema = make(map[string]interface{})
	}

	return &config, nil
}

// Helper functions for null conversions
func convertNullString(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

func convertToNullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
