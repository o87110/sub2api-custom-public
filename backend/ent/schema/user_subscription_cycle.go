package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// UserSubscriptionCycle stores current and queued entitlement periods. Quota
// windows remain on UserSubscription and are intentionally independent.
type UserSubscriptionCycle struct {
	ent.Schema
}

func (UserSubscriptionCycle) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "user_subscription_cycles"}}
}

func (UserSubscriptionCycle) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (UserSubscriptionCycle) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("subscription_id"),
		field.Time("starts_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("ends_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("status").MaxLen(20).Default("pending"),
		field.String("source_type").MaxLen(32).Default("assignment"),
		field.String("source_ref").Optional().Nillable().MaxLen(255),
		field.Bool("manual_bulk_quota_reset_enabled").Default(false),
		field.Float("final_usage_usd").SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}).Default(0),
		field.Int64("final_manual_quota_reset_count").Default(0),
		field.Time("completed_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (UserSubscriptionCycle) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("subscription", UserSubscription.Type).
			Ref("cycles").
			Field("subscription_id").
			Unique().
			Required(),
	}
}

func (UserSubscriptionCycle) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("subscription_id", "starts_at"),
		index.Fields("subscription_id", "status"),
		index.Fields("source_type", "source_ref").
			Unique().
			Annotations(entsql.IndexWhere("source_ref IS NOT NULL")),
		index.Fields("subscription_id").
			Unique().
			Annotations(entsql.IndexWhere("status = 'current'")),
	}
}
