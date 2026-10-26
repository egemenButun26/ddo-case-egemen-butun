export type Lot = {
  id?: number;
  code?: string;
  name?: string;
  capacity?: number;
};

export type Doluluk = {
  lot_id?: number;
  code?: string;
  capacity?: number;
  occupied?: number;
  active_reservations?: number;
  available?: number;
};

export type Olay = {
  id?: number;
  lot_id?: number;
  camera_id?: number;
  direction?: string;
  plate_raw?: string;
  plate_norm?: string;
  captured_at?: string;
  outcome?: string;
  session_id?: number | null;
};

export type Inceleme = {
  id?: number;
  reason?: string;
  read_plate?: string;
  event_id?: number;
  captured_at?: string;
  suggested_session_id?: number | null;
  confidence?: number;
};

export type KaraListeKaydi = {
  id?: number;
  plate?: string;
  reason?: string;
  active?: boolean;
  created_at?: string;
};

export type Plan = {
  code?: string;
  lot_id?: number;
  monthly_price_kurus?: number;
};

export type Abonelik = {
  id?: number;
  plan_code?: string;
  plate?: string;
  status?: string;
  current_period_start?: string;
  current_period_end?: string;
};

export type Fatura = {
  id?: number;
  subscription_id?: number;
  kind?: string;
  period_start?: string;
  amount_kurus?: number;
  status?: string;
  attempts?: number;
  next_retry_at?: string;
};

export type BorcKalemi = {
  id?: number;
  session_id?: number;
  lot_id?: number;
  amount_kurus?: number;
  paid_kurus?: number;
  remaining_kurus?: number;
  status?: string;
  created_at?: string;
};

export type Borc = {
  plate?: string;
  total_kurus?: number;
  items?: BorcKalemi[];
};

export type Odeme = {
  id?: number;
  session_id?: number;
  amount_kurus?: number;
  status?: string;
  provider_ref?: string;
  requested_at?: string;
  paid_at?: string;
  late_approval?: boolean;
};

export type Rezervasyon = {
  id?: number;
  lot_id?: number;
  plate?: string;
  status?: string;
  reserved_at?: string;
  expires_at?: string;
};

export type SayacKaymasi = {
  id?: number;
  lot_id?: number;
  stored?: number;
  recomputed?: number;
  drifted?: number;
  corrected?: boolean;
  created_at?: string;
};

export type Sayfali<T> = {
  items?: T[];
  next_after_id?: number | null;
};

export type SayacYenidenSayim = {
  stored?: number;
  recomputed?: number;
  drifted?: number;
  corrected?: number;
};

export type GunlukRapor = {
  day?: string;
  lot_id?: number;
  entries?: number;
  exits?: number;
  closed_sessions?: number;
  charged_kurus?: number;
  collected_kurus?: number;
  open_debt_kurus?: number;
  open_reviews?: number;
};

export type Mutabakat = {
  day?: string;
  our_total_kurus?: number;
  provider_total_kurus?: number;
  diff_kurus?: number;
  matched?: number;
  missing_in_provider?: unknown[];
  missing_in_ours?: unknown[];
  amount_mismatch?: unknown[];
};
