package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// UserAffiliate maps the existing user_affiliates aggregate table so financial
// adjustments can use typed Ent mutations instead of raw write SQL.
type UserAffiliate struct {
	ent.Schema
}

func (UserAffiliate) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "user_affiliates"}}
}

func (UserAffiliate) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("user_id"),
		field.String("aff_code").MaxLen(32),
		field.Bool("aff_code_custom").Default(false),
		field.Float("aff_rebate_rate_percent").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "decimal(5,2)"}),
		field.Int64("inviter_id").Optional().Nillable(),
		field.Int("aff_count").Default(0),
		field.Float("aff_quota").Default(0).SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.Float("aff_frozen_quota").Default(0).SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.Float("aff_history_quota").Default(0).SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (UserAffiliate) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("aff_code").Unique(),
		index.Fields("inviter_id"),
		index.Fields("aff_quota"),
	}
}
