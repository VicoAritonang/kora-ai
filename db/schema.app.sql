-- Kora AI — application schema (PostgreSQL 15+, tested on Neon).
-- Source: docs/DATABASE_SCHEMA.md. Additions vs. the doc are marked [impl].
-- This file is concatenated with db/river.sql into db/schema.sql by
-- scripts/build-schema.mjs; edit this file, not db/schema.sql.

create extension if not exists citext;
create extension if not exists pgcrypto;

-- ---------------------------------------------------------------- enums ---
create type message_role     as enum ('user', 'assistant', 'notice');
create type run_status       as enum ('running', 'completed', 'failed', 'cancelled', 'blocked');
create type run_mode         as enum ('plan_trip', 'budget_to_destination', 'refine', 'info', 'chitchat', 'out_of_scope');
create type offer_kind       as enum ('flight', 'stay', 'activity');
create type package_tier     as enum ('hemat', 'seimbang', 'nyaman');
create type package_status   as enum ('proposed', 'selected', 'stale');
-- [impl] 'unbounded': no budget given, feasibility not computed (MODULE_SPEC §9).
create type feasibility      as enum ('fits', 'tight', 'over_budget', 'unbounded');
create type item_category    as enum ('flight', 'stay', 'activity', 'meal', 'local_transport', 'visa', 'insurance', 'other');
create type price_kind       as enum ('live', 'estimate');
create type event_visibility as enum ('public', 'internal');

-- ------------------------------------------------------------ identity ---
create table users (
    id              uuid primary key,
    email           citext unique,
    display_name    text,
    is_guest        boolean not null default true,
    home_city_code  text,
    locale          text not null default 'id',
    memory_enabled  boolean not null default false,
    created_at      timestamptz not null default now(),
    updated_at      timestamptz not null default now(),
    last_seen_at    timestamptz,
    deleted_at      timestamptz
);
create index users_guest_seen_idx on users (last_seen_at) where is_guest;

create table auth_identities (
    id               uuid primary key,
    user_id          uuid not null references users(id) on delete cascade,
    provider         text not null check (provider in ('magic_link', 'google')),
    provider_subject text not null,
    created_at       timestamptz not null default now(),
    unique (provider, provider_subject)
);

create table auth_sessions (
    id          uuid primary key,
    user_id     uuid not null references users(id) on delete cascade,
    token_hash  bytea not null unique,
    user_agent  text,
    ip_hash     bytea,
    created_at  timestamptz not null default now(),
    expires_at  timestamptz not null,
    revoked_at  timestamptz
);
create index auth_sessions_user_idx on auth_sessions (user_id);
create index auth_sessions_expires_idx on auth_sessions (expires_at);

create table magic_link_tokens (
    id          uuid primary key,
    email       citext not null,
    token_hash  bytea not null unique,
    expires_at  timestamptz not null,
    used_at     timestamptz,
    created_at  timestamptz not null default now()
);
create index magic_link_tokens_email_idx on magic_link_tokens (email, created_at desc);

-- -------------------------------------------------------- conversations ---
create table conversations (
    id                    uuid primary key,
    user_id               uuid not null references users(id) on delete cascade,
    title                 text,
    trip_state            jsonb not null default '{}',
    trip_state_version    integer not null default 0,
    summary               text,
    summary_upto_seq      bigint not null default 0,
    selected_package_id   uuid,
    guard_session         jsonb,
    created_at            timestamptz not null default now(),
    updated_at            timestamptz not null default now(),
    last_message_at       timestamptz
);
create index conversations_user_recent_idx on conversations (user_id, last_message_at desc);

create table messages (
    id               uuid primary key,
    conversation_id  uuid not null references conversations(id) on delete cascade,
    seq              bigint not null,
    role             message_role not null,
    content          text not null,
    blocks           jsonb,
    run_id           uuid,
    created_at       timestamptz not null default now(),
    unique (conversation_id, seq)
);

-- --------------------------------------------------------- runs/events ---
create table runs (
    id                  uuid primary key,
    conversation_id     uuid not null references conversations(id) on delete cascade,
    user_message_id     uuid not null references messages(id) on delete cascade,
    status              run_status not null default 'running',
    mode                run_mode,
    step_count          integer not null default 0,
    input_tokens        bigint not null default 0,
    cached_tokens       bigint not null default 0,
    output_tokens       bigint not null default 0,
    cost_usd_micros     bigint not null default 0,
    flags               text[] not null default '{}',
    error_code          text,
    config_snapshot     jsonb not null,
    started_at          timestamptz not null default now(),
    finished_at         timestamptz
);
create index runs_conversation_idx on runs (conversation_id, started_at desc);
create unique index runs_one_active_per_conversation on runs (conversation_id) where status = 'running';
create index runs_running_idx on runs (started_at) where status = 'running';

create table run_events (
    id               bigint generated always as identity,
    run_id           uuid not null,
    seq              integer not null,
    type             text not null,
    visibility       event_visibility not null default 'internal',
    step             text,
    payload          jsonb not null,
    model            text,
    input_tokens     integer,
    cached_tokens    integer,
    output_tokens    integer,
    cost_usd_micros  bigint,
    latency_ms       integer,
    created_at       timestamptz not null default now(),
    primary key (id, created_at)
) partition by range (created_at);
create index run_events_run_seq_idx on run_events (run_id, seq);

-- [impl] Creates the monthly partition containing `month`; the worker calls
-- it ahead of time (job "partition_maintenance").
create or replace function kora_ensure_run_events_partition(month date) returns void
language plpgsql as $$
declare
    start_d date := date_trunc('month', month)::date;
    end_d   date := (date_trunc('month', month) + interval '1 month')::date;
    name    text := 'run_events_' || to_char(start_d, 'YYYYMM');
begin
    if to_regclass(name) is null then
        execute format('create table %I partition of run_events for values from (%L) to (%L)', name, start_d, end_d);
    end if;
end $$;

do $$
declare m date;
begin
    for m in select generate_series(date_trunc('month', now())::date, (date_trunc('month', now()) + interval '6 months')::date, interval '1 month')::date loop
        perform kora_ensure_run_events_partition(m);
    end loop;
end $$;

create table tool_artifacts (
    id          uuid primary key,
    run_id      uuid not null references runs(id) on delete cascade,
    tool_name   text not null,
    args        jsonb not null,
    result      jsonb not null,
    digest      text not null,
    created_at  timestamptz not null default now()
);
create index tool_artifacts_run_idx on tool_artifacts (run_id);
create index tool_artifacts_created_idx on tool_artifacts (created_at);

-- -------------------------------------------------------- trip requests ---
create table trip_requests (
    id                 uuid primary key,
    conversation_id    uuid not null references conversations(id) on delete cascade,
    run_id             uuid not null references runs(id) on delete cascade,
    origin_code        text,
    destination_code   text,
    budget_amount      bigint,
    budget_currency    char(3),
    budget_idr         bigint,
    date_from          date,
    date_to            date,
    date_window        jsonb,
    nights             integer check (nights between 1 and 60),
    pax_adults         integer not null default 1 check (pax_adults between 1 and 9),
    pax_children       integer not null default 0 check (pax_children between 0 and 8),
    interests          text[] not null default '{}',
    special_requests   jsonb not null default '[]',
    assumptions        jsonb not null default '[]',
    created_at         timestamptz not null default now(),
    check (budget_amount is not null or destination_code is not null)
);
create index trip_requests_run_idx on trip_requests (run_id);

-- --------------------------------------------------------------- offers ---
create table offers (
    id              uuid primary key,
    kind            offer_kind not null,
    supplier        text not null,
    supplier_ref    text not null,
    search_key      text not null,
    price_amount    bigint not null check (price_amount >= 0),
    price_currency  char(3) not null,
    price_idr       bigint not null check (price_idr >= 0),
    fx_rate         numeric(18,8) not null,
    fx_date         date not null,
    summary         jsonb not null,
    raw             jsonb,
    deeplink_url    text,
    fetched_at      timestamptz not null,
    expires_at      timestamptz not null,
    created_at      timestamptz not null default now(),
    unique (supplier, supplier_ref, fetched_at)
);
create index offers_search_idx on offers (kind, search_key, fetched_at desc);
create index offers_expires_idx on offers (expires_at);

-- ----------------------------------------------------------- cost norms ---
create table cost_norms (
    id                uuid primary key,
    destination_code  text not null,
    category          text not null check (category in
                        ('meal', 'local_transport_daily', 'transport_link', 'attraction', 'visa', 'other')),
    tier              text check (tier in ('hemat', 'sedang', 'nyaman')),
    label             text not null,
    unit              text not null check (unit in
                        ('per_meal', 'per_day', 'per_ride', 'per_person', 'per_trip')),
    amount            bigint not null check (amount >= 0),
    currency          char(3) not null,
    child_factor      numeric(4,2) check (child_factor between 0 and 1),
    details           jsonb not null default '{}',
    source_label      text not null,
    source_url        text,
    verified_at       date not null,
    valid_from        date,
    valid_to          date,
    created_at        timestamptz not null default now(),
    updated_at        timestamptz not null default now()
);
create index cost_norms_lookup_idx on cost_norms (destination_code, category, tier);

-- ------------------------------------------------- price index and FX ---
create table price_cache (
    id                 uuid primary key,
    kind               text not null check (kind in ('flight_route_month', 'stay_city_month')),
    origin_code        text not null default '',  -- [impl] '' instead of NULL so the unique key works for stays
    destination_code   text not null,
    month              date not null,
    tier               text not null default '',  -- [impl] '' for flights
    min_idr            bigint not null,
    median_idr         bigint not null,
    sample_count       integer not null,
    source             text not null,
    refreshed_at       timestamptz not null,
    expires_at         timestamptz not null,
    unique (kind, origin_code, destination_code, month, tier)
);
create index price_cache_dest_idx on price_cache (destination_code, month);

create table fx_rates (
    base        char(3) not null,
    quote       char(3) not null,
    rate_date   date not null,
    rate        numeric(18,8) not null check (rate > 0),
    source      text not null,
    created_at  timestamptz not null default now(),
    primary key (base, quote, rate_date)
);

-- ------------------------------------------------------------- packages ---
create table packages (
    id                uuid primary key,
    conversation_id   uuid not null references conversations(id) on delete cascade,
    run_id            uuid not null references runs(id) on delete cascade,
    trip_request_id   uuid not null references trip_requests(id) on delete cascade,
    tier              package_tier not null,
    title             text not null,
    status            package_status not null default 'proposed',
    feasibility       feasibility not null,
    budget_idr        bigint,
    total_idr         bigint not null check (total_idr >= 0),
    live_idr          bigint not null,
    estimate_idr      bigint not null,
    buffer_idr        bigint not null default 0,
    shortfall_idr     bigint not null default 0,
    assumptions       jsonb not null default '[]',
    alternatives      jsonb not null default '[]',
    builder_version   text not null,
    flight_offer_id   uuid references offers(id) on delete set null,  -- [impl] combination, for re-pricing
    stay_offer_id     uuid references offers(id) on delete set null,  -- [impl]
    created_at        timestamptz not null default now(),
    check (total_idr = live_idr + estimate_idr)
);
create index packages_conversation_idx on packages (conversation_id, created_at desc);
create index packages_run_idx on packages (run_id);

alter table conversations
    add constraint conversations_selected_package_fk
    foreign key (selected_package_id) references packages(id) on delete set null;

create table package_days (
    id           uuid primary key,
    package_id   uuid not null references packages(id) on delete cascade,
    day_index    integer not null check (day_index >= 0),
    date         date,
    title        text not null,
    summary      text,
    area         text,                                   -- [impl]
    unique (package_id, day_index)
);

create table package_items (
    id              uuid primary key,
    package_id      uuid not null references packages(id) on delete cascade,
    day_index       integer,
    sort_order      integer not null,
    category        item_category not null,
    title           text not null,
    quantity        integer not null default 1 check (quantity > 0),
    unit_amount     bigint not null check (unit_amount >= 0),
    currency        char(3) not null,
    amount_idr      bigint not null check (amount_idr >= 0),
    fx_rate         numeric(18,8) not null,
    fx_date         date,                                -- [impl] invariant 8 (rate age)
    price_kind      price_kind not null,
    offer_id        uuid references offers(id) on delete restrict,
    norm_id         uuid references cost_norms(id) on delete restrict,
    price_cache_id  uuid references price_cache(id) on delete set null,  -- [impl] estimates from the cost index
    source_label    text not null,
    source_url      text,
    priced_at       timestamptz not null,
    expires_at      timestamptz,
    details         jsonb not null default '{}',
    check (price_kind <> 'live' or (offer_id is not null and expires_at is not null)),
    check (price_kind <> 'estimate' or norm_id is not null or source_url is not null or price_cache_id is not null)
);
create index package_items_package_idx on package_items (package_id, day_index, sort_order);
create index package_items_offer_idx on package_items (offer_id) where offer_id is not null;

-- -------------------------------------------------- user interactions ---
create table outbound_clicks (
    id               uuid primary key,
    user_id          uuid references users(id) on delete set null,
    package_item_id  uuid references package_items(id) on delete set null,
    offer_id         uuid references offers(id) on delete set null,
    supplier         text not null,
    created_at       timestamptz not null default now()
);
create index outbound_clicks_created_idx on outbound_clicks (created_at);

create table reminders (
    id               uuid primary key,
    user_id          uuid not null references users(id) on delete cascade,
    conversation_id  uuid references conversations(id) on delete cascade,
    kind             text not null check (kind in ('booking_window_open', 'price_recheck')),
    payload          jsonb not null,
    trigger_at       timestamptz not null,
    status           text not null default 'pending' check (status in ('pending', 'sent', 'cancelled', 'failed')),
    attempts         integer not null default 0,           -- [impl] email retry with backoff
    created_at       timestamptz not null default now()
);
create index reminders_due_idx on reminders (trigger_at) where status = 'pending';

create table feedback (
    id           uuid primary key,
    user_id      uuid references users(id) on delete set null,
    message_id   uuid references messages(id) on delete cascade,
    package_id   uuid references packages(id) on delete cascade,
    rating       smallint not null check (rating in (-1, 1)),
    reason       text check (reason in ('price_wrong', 'not_relevant', 'too_expensive', 'missing_info', 'other')),
    comment      text,
    created_at   timestamptz not null default now(),
    check (message_id is not null or package_id is not null)
);

create table idempotency_keys (
    key            text not null,
    user_id        uuid not null references users(id) on delete cascade,
    request_hash   bytea not null,
    response       jsonb,
    created_at     timestamptz not null default now(),
    primary key (user_id, key)
);
create index idempotency_keys_created_idx on idempotency_keys (created_at);
