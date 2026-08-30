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

// UserAffiliateLedger maps the existing append-only affiliate accrual and
// transfer ledger. Reversal code uses it for typed row locks; it never deletes
// or rewrites the original accrual.
type UserAffiliateLedger struct {
	ent.Schema
}

func (UserAffiliateLedger) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "user_affiliate_ledger"}}
}

func (UserAffiliateLedger) Fields() []ent.Field {
	money := func(name string) ent.Field {
		return field.Float(name).Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"})
	}
	return []ent.Field{
		field.Int64("user_id"),
		field.String("action").MaxLen(32),
		field.Float("amount").SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.Int64("source_user_id").Optional().Nillable(),
		field.Int64("source_order_id").Optional().Nillable(),
		field.Time("frozen_until").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		money("balance_after"),
		money("aff_quota_after"),
		money("aff_frozen_quota_after"),
		money("aff_history_quota_after"),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (UserAffiliateLedger) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id"),
		index.Fields("action"),
		index.Fields("source_order_id"),
		index.Fields("action", "source_order_id", "user_id", "source_user_id", "created_at"),
	}
}
