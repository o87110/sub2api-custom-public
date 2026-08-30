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

// UserAffiliateReversal is the immutable financial audit for one reversed
// affiliate accrual. The original accrual and payment order remain intact.
type UserAffiliateReversal struct {
	ent.Schema
}

func (UserAffiliateReversal) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "user_affiliate_reversals"}}
}

func (UserAffiliateReversal) Fields() []ent.Field {
	money := func(name string) ent.Field {
		return field.Float(name).Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"})
	}
	return []ent.Field{
		field.Int64("source_ledger_id").Optional().Nillable(),
		field.Int64("source_order_id"),
		field.Int64("inviter_user_id"),
		field.Int64("invitee_user_id"),
		field.Float("rebate_amount").SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		money("frozen_quota_deducted"),
		money("available_quota_deducted"),
		money("balance_deducted"),
		money("total_recharged_deducted"),
		money("balance_before"),
		money("balance_after"),
		money("aff_quota_before"),
		money("aff_quota_after"),
		money("aff_frozen_quota_before"),
		money("aff_frozen_quota_after"),
		money("aff_history_quota_before"),
		money("aff_history_quota_after"),
		money("total_recharged_before"),
		money("total_recharged_after"),
		field.Bool("snapshot_available").Default(true),
		field.String("reason").SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Int64("operator_user_id").Optional().Nillable(),
		field.String("operation_key_hash").MaxLen(64).Optional().Nillable(),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (UserAffiliateReversal) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("source_ledger_id").Unique(),
		index.Fields("source_order_id").Unique(),
		index.Fields("inviter_user_id", "created_at"),
		index.Fields("invitee_user_id", "created_at"),
		index.Fields("operation_key_hash"),
	}
}
