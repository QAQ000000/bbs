// 后端 API DTO 类型。
// 资源 ID 一律是十进制字符串；时间使用带时区的 RFC3339；零值时间由 format 层识别。

export interface PageMeta {
  page: number;
  pageSize: number;
  total: number;
  totalPages: number;
  /** 游标分页流（关注信息流）返回；普通分页列表不带这些字段。 */
  pagination?: string;
  stream?: string;
  hasMore?: boolean;
  nextCursor?: string;
  followingCount?: number;
}

export interface ListEnvelope<T> {
  data: T;
  meta?: PageMeta;
}

export interface ApiEnvelope<T> {
  data: T;
}

export interface ApiErrorBody {
  error: {
    code: string;
    message: string;
    fields?: Record<string, string>;
  };
  requestId?: string;
}

export type Capabilities = Record<string, boolean>;

export interface MemberBadge {
  label: string;
  icon: string;
  color: string;
  background: string;
}

export interface MemberLevel {
  id: number;
  name: string;
  rank: number;
  badge: MemberBadge;
}

export interface EquippedTitle {
  id: string;
  name: string;
  color?: string;
  icon?: string;
}

export interface PublicUser {
  id: string;
  username: string;
  groupId: number;
  postCount: number;
  signature: string;
  createdAt: string;
  avatarUrl: string;
}

export interface CurrentUser extends PublicUser {
  email?: string;
  emailVerified?: boolean;
  mustChangePassword?: boolean;
  // 会员 / 成长字段由 /me 返回，按需读取。
  points?: number;
  experience?: number;
  level?: MemberLevel;
  equippedTitle?: EquippedTitle | null;
}

export interface SessionView {
  user: CurrentUser | null;
  csrfToken: string;
  setupRequired: boolean;
}

export interface SiteView {
  name: string;
  logo: string;
  footerText: string;
  threadsPerPage: number;
  postsPerPage: number;
  registerEnabled: boolean;
  siteClosed: boolean;
  siteClosedReason: string;
  uploadEnabled: boolean;
  maxImageMB: number;
  maxFileMB: number;
  captchaEnabled: boolean;
  requireConsent: boolean;
  emailVerificationRequired: boolean;
  termsContent: string;
  privacyContent: string;
}

export interface ForumSummary {
  id: string;
  categoryId: string;
  name: string;
  description: string;
  threadCount: number;
  postCount: number;
  todayCount: number;
  lastPostAt: string;
  lastThreadId: string;
  lastThreadTitle: string;
  lastPostAuthor: string;
  moderators: string;
  capabilities?: Capabilities;
}

export interface CategoryWithForums {
  id: string;
  name: string;
  forums: ForumSummary[];
}

export interface SiteStats {
  todayPosts: number;
  yesterdayPosts: number;
  totalPosts: number;
  totalThreads: number;
  members: number;
}

export interface Announcement {
  id: string;
  authorId: string;
  author: string;
  content: string;
  enabled: boolean;
  createdAt: string;
}

export interface ThreadTag {
  id: string;
  name: string;
  slug?: string;
  color?: string;
}

// 投票选项 ID 是整数（与其它资源 ID 的字符串约定不同）。
export interface PollOption {
  id: number;
  text: string;
  votes: number;
}

export interface PollView {
  threadId: string;
  question: string;
  maxChoices: number;
  state: string;
  closesAt: string;
  createdAt: string;
  closed: boolean;
  voters: number;
  options: PollOption[];
  myChoices: number[];
}

/** 主题列表附带的投票摘要；选项与计票细节需读详情接口。 */
export interface PollSummary {
  question: string;
  state: string;
  maxChoices: number;
  voters: number;
  closesAt: string;
  closed: boolean;
  hasVoted: boolean;
}

export interface BountyView {
  threadId: string;
  ownerId: string;
  amount: number;
  durationHours: number;
  state: string;
  closesAt: string;
  createdAt: string;
  settledAt: string | null;
  recipientId: string;
  postId: string;
  ruleVersion: number;
  note: string;
}

/** 主题列表附带的悬赏摘要；不含退款诊断与私有余额。 */
export interface BountySummary {
  amount: number;
  state: string;
  closesAt: string;
  expired: boolean;
  postId: string;
}

export interface PointsAccount {
  userId: string;
  balance: number;
  frozen: number;
  available: number;
  debt: number;
  version: number;
}

export interface EngagementRules {
  poll: { enabled: boolean; maxOptions: number; maxDays: number };
  bounty: { enabled: boolean; minPoints: number; maxPoints: number; maxDays: number };
  checkin: { enabled: boolean; experience: number; points: number; timeZone: string };
}

export interface SettingField {
  name: string;
  legacyName: string;
  type: 'string' | 'integer' | 'boolean';
  min: number;
  max: number;
}

export interface SiteSettingsAdmin {
  version: number;
  siteName: string;
  threadsPerPage: number;
  postsPerPage: number;
  registerEnabled: boolean;
  siteClosed: boolean;
  siteClosedReason: string;
  moderateEnabled: boolean;
  uploadEnabled: boolean;
  maxImageMB: number;
  maxFileMB: number;
  uploadMaxDiskGB: number;
  captchaEnabled: boolean;
  emailVerifyEnabled: boolean;
  requireConsent: boolean;
  termsContent: string;
  privacyContent: string;
  siteLogo: string;
  footerText: string;
  analyticsRetentionDays: number;
  reportTimeZone: string;
  [key: string]: unknown;
}

export interface SettingsSchema {
  fields: SettingField[];
  defaults: SiteSettingsAdmin;
  permission: string;
  versionRequired: boolean;
  requiresRestart: boolean;
  stringLengthUnit: string;
  dependencies: Record<string, string[]>;
}

export interface SettingsStatus {
  version: number;
  valid: boolean;
  issues: { field: string; message: string }[];
  effective: Record<string, unknown> | null;
  smtpEnabled: boolean;
}

export interface AdminUserView {
  id: string;
  username: string;
  email: string;
  groupId: number;
  postCount: number;
  createdAt: string;
  lastLoginAt: string;
  bannedUntil: string | null;
  banReason: string;
  isBanned: boolean;
  blockedUntil: string | null;
  isBlocked: boolean;
  level?: MemberLevel;
}

export interface ReplyTarget {
  id: string;
  available: boolean;
  floor?: number;
  authorId?: string;
  authorName?: string;
}

export interface ThreadSummary {
  id: string;
  forumId: string;
  authorId: string;
  authorName: string;
  title: string;
  sticky: number;
  digest: boolean;
  closed: boolean;
  postCount: number;
  viewCount: number;
  createdAt: string;
  lastPostAt: string;
  /** 真实内容更新时间（含楼层编辑）；仅主题详情接口返回。 */
  updatedAt?: string;
  lastPostUserId: string;
  lastPostName: string;
  firstPostId: string;
  pending: boolean;
  pendingReason: string;
  authorLevel?: MemberLevel;
  capabilities?: Capabilities;
  equippedTitle?: EquippedTitle | null;
  acceptedPostId?: string;
  tags?: ThreadTag[];
  poll?: PollSummary | null;
  bounty?: BountySummary | null;
  favorite?: boolean;
  subscribed?: boolean;
  /** 列表摘要：纯文本、长度受控；后端按可见首楼投影，为空则不显示。 */
  excerpt?: string;
  /** 可选封面：仅站内受控媒体，为空时展示纯文字信息流。 */
  coverUrl?: string;
}

export interface ForumDetail extends ForumSummary {
  capabilities: Capabilities;
  /** 仅登录后由详情接口返回；undefined 表示未知（旧后端或取数失败）。 */
  subscribed?: boolean;
}

export interface ThreadListData {
  threads: ThreadSummary[];
  stickies: ThreadSummary[];
}

/** 关注聚合流的游标分页元信息。 */
export interface FeedMeta {
  pagination: 'cursor' | string;
  stream: 'forums' | 'users' | string;
  pageSize: number;
  hasMore: boolean;
  nextCursor: string;
  /** 当前账号已关注的版块 / 用户数量，用于区分“无关注”与“暂无内容”。 */
  followingCount: number;
}

export interface FollowFeedData {
  threads: ThreadSummary[];
}

/** 公开积分余额榜：榜单来自 Worker 快照，不表示累计获得积分。 */
export interface LeaderboardEntry {
  rank: number;
  userId: string;
  username: string;
  avatarUrl: string;
  points: number;
}

export interface LeaderboardView {
  period: 'balance' | 'day' | 'week' | 'month';
  periodStart?: string;
  periodEnd?: string;
  asOf?: string;
  complete?: boolean;
  timeZone: string;
  status: 'ready' | 'stale' | 'unavailable' | string;
  generatedAt: string | null;
  stale: boolean;
  ageSeconds: number;
  staleAfterSeconds: number;
  refreshIntervalSeconds: number;
  entries: LeaderboardEntry[];
}

export interface SetupState {
  required: boolean;
  database: 'up';
  schema: number;
  smtpEnabled: boolean;
  secureCookies: boolean;
}

export interface PopularView {
  since: string;
  generatedAt: string;
  threads: { id: string; title: string; replies: number; views: number }[];
  authors: { userId: string; username: string; posts: number; likes: number }[];
}

export interface UploadView {
  id: string;
  postId: string;
  name: string;
  url: string;
  size: number;
  mime: string;
}

export interface PostView {
  id: string;
  threadId: string;
  authorId: string;
  authorName: string;
  authorGroup: number;
  floor: number;
  content: string;
  createdAt: string;
  editedAt: string;
  hasEdited: boolean;
  pending: boolean;
  pendingReason: string;
  likeCount: number;
  version: number;
  capabilities: Capabilities;
  authorLevel?: MemberLevel;
  attachments: UploadView[];
  viewerHasLiked: boolean;
  replyTo: ReplyTarget | null;
  equippedTitle?: EquippedTitle | null;
  accepted: boolean;
  maskedIp?: string;
}

export interface HomeView {
  categories: CategoryWithForums[];
  stats: SiteStats;
  threads: ThreadSummary[];
  announcements: Announcement[];
}

export interface SearchHit {
  threadId: string;
  title: string;
  forumId: string;
  forumName: string;
  authorName: string;
  createdAt: string;
  excerpt: string;
}

export interface UserProfileView {
  user: PublicUser & { level?: MemberLevel; equippedTitle?: EquippedTitle | null };
  threads: ThreadSummary[];
  reputation: { posts: number; likes: number };
  /** 仅登录后由用户详情接口返回：当前用户是否已关注该用户。 */
  following?: boolean;
}

export interface TagView {
  id: string;
  name: string;
  slug: string;
  description: string;
  color: string;
  status: string;
  threadCount: number;
  version?: number;
  aliases?: string[];
  /** 仅登录后由标签详情接口返回。 */
  subscribed?: boolean;
}

export interface SubscriptionView {
  kind: 'thread' | 'forum' | 'tag';
  targetId: string;
  name?: string;
  enabled: boolean;
  notifyInApp: boolean;
  notifyEmail: boolean;
  mutedUntil: string | null;
  createdAt: string;
}

export interface FollowUserView {
  id: string;
  username: string;
  followedAt: string;
  /** 列表接口登录后总是显式返回，false 表示确定未关注。 */
  following?: boolean;
}

export interface NotificationView {
  id: string;
  fromUserId: string;
  fromName: string;
  type: string;
  threadId: string;
  postId: string;
  excerpt: string;
  read: boolean;
  createdAt: string;
  scope: string;
  payload: string;
}

export interface NotificationSummary {
  total: number;
  unread: number;
}

export interface ConversationView {
  id: string;
  otherId: string;
  otherName: string;
  lastMessageAt: string;
  unread: number;
  blocked: boolean;
  waitingForReply: boolean;
}

export interface MessageView {
  id: string;
  conversationId: string;
  senderId: string;
  body: string;
  createdAt: string;
}

export interface SmileyCode {
  code: string;
  /** 内置 Unicode 表情时存在。 */
  unicode?: string;
  /** 图片表情包名与文件名时存在，地址为 /smiley/<pkg>/<file>。 */
  pkg?: string;
  file?: string;
}
export interface SmileyGroup {
  Name: string;
  Dir: string;
  Codes: SmileyCode[];
}

export interface DatabasePoolStats {
  max: number;
  total: number;
  acquired: number;
  idle: number;
  acquireCount: number;
  acquireDurationMs: number;
  emptyAcquireCount: number;
  canceledAcquireCount: number;
}

export interface SessionDevice {
  id: string;
  name: string;
  userAgent: string;
  maskedIp: string;
  createdAt: string;
  lastSeenAt: string;
  expiresAt: string;
  revokedAt: string | null;
  current: boolean;
  status: string;
}

export interface SessionList {
  currentSessionId: string;
  items: SessionDevice[];
}

export interface MfaStatus {
  enabled: boolean;
  recoveryCodesRemaining: number;
}

export interface NotificationPreferences {
  mentions: boolean;
  replies: boolean;
  acceptance: boolean;
  membership: boolean;
  titles: boolean;
  moderation: boolean;
  reports: boolean;
  email: boolean;
  subscriptions: boolean;
}

export interface OwnContentItem {
  id: string;
  threadId: string;
  forumId: string;
  floor: number;
  subject: string;
  content: string;
  status: string;
  moderationNote: string;
  parentAvailable: boolean;
  createdAt: string;
}

export interface FavoriteView extends ThreadSummary {
  lastFloor: number;
  hasNew: boolean;
}

export interface CheckinRecord {
  userId: string;
  day: string;
  streak: number;
  experience: number;
  points: number;
  ruleVersion: number;
  timeZone: string;
  createdAt: string;
}

export interface CheckinStatus {
  day: string;
  timeZone: string;
  enabled: boolean;
  checkedIn: boolean;
  streak: number;
  checkin: CheckinRecord | null;
}

export interface CheckinHistory {
  items: CheckinRecord[];
  nextBefore: string;
}

// ---- 后台 ----
export interface AdminDashboard {
  banned: number;
  recycle: number;
  subscriptions: number;
  uploadBytes: number;
  uploadLimitGB: number;
  uptimeSeconds: number;
  dbSize: string;
  goVersion: string;
  stats: SiteStats;
  databasePool: DatabasePoolStats;
}

export interface AdminDiagnostics {
  databasePool: DatabasePoolStats;
  databaseWorkload: {
    transactions: number;
    readIO: number;
    hitIO: number;
    cacheHitRatio: number;
  };
  forumStats: { asyncPublication: boolean; pending: number; retrying: number; oldestAgeSeconds: number };
  lockWaits: number;
  searchIndex: { pending: number; retrying: number; oldestAgeSeconds: number };
}

export interface AdminModerateQueue {
  threads: ThreadSummary[];
  posts: PostView[];
  reports: ReportRow[];
}

export interface ReportRow {
  id: string;
  postId: string;
  reporterId: string;
  reporter: string;
  reason: string;
  createdAt: string;
  excerpt: string;
  floor: number;
  pending: boolean;
  deleted: boolean;
  tId: string;
  threadTtl: string;
  authorName: string;
}
// ---- 后台：会员等级 / 称号 / 互动配置 / 投票 / 悬赏 ----

export interface LevelBadge {
  label: string;
  icon: string;
  color: string;
  background: string;
}

export interface MemberLimits {
  threadsPerDay: number;
  repliesPerDay: number;
  uploadsPerDay: number;
  uploadBytesPerDay: number;
  imageBytes: number;
  fileBytes: number;
  attachmentsPerPost: number;
  editMinutes: number;
  signatureLength: number;
}

export interface MemberLevel {
  id: number;
  name: string;
  rank: number;
  automatic: boolean;
  experience: number;
  daysVisited: number;
  postsRead: number;
  postCount: number;
  emailVerified: boolean;
  permissions: Record<string, boolean>;
  limits: MemberLimits;
  badge: LevelBadge;
}

export interface GrowthRule {
  enabled: boolean;
  points: number;
  dailyCap: number;
  reverse: boolean;
}

export interface ForumMembership {
  forumId: string;
  minimumLevel: number;
  membersOnly: boolean;
  denied: string[];
}

export interface MembershipConfig {
  guestPermissions: Record<string, boolean>;
  version: number;
  levels: MemberLevel[];
  rules: Record<string, GrowthRule>;
  forums: ForumMembership[];
}

export interface MemberPreview {
  affectedUsers: number;
  token: string;
  users: number;
  upgrades: number;
  locked: number;
  configVersion: number;
}

export interface MemberStateView {
  restricted: boolean;
  userId: string;
  levelId: number;
  experience: number;
  locked: boolean;
  version: number;
  level: MemberLevel;
  nextLevel: MemberLevel | null;
  daysVisited: number;
  postsRead: number;
  postCount: number;
  emailVerified: boolean;
}

export interface MemberAdjustment {
  version: number;
  levelId?: number | null;
  locked?: boolean | null;
  delta: number;
  reason: string;
  key: string;
}

export interface ExperienceEntry {
  id: string;
  source: string;
  kind: string;
  delta: number;
  reversed: boolean;
  ruleVersion: number;
  reason: string;
  createdAt: string;
}

export interface TitleCondition {
  metric: string;
  target: number;
  forumId: string;
}

export interface TitleDefinition {
  id: string;
  version: number;
  name: string;
  description: string;
  badge: LevelBadge;
  status: string;
  mode: string;
  match: string;
  conditions: TitleCondition[];
  startsAt: string | null;
  endsAt: string | null;
  durationDays: number;
  expiresAt: string | null;
  sort: number;
}

export interface TitlePreview {
  eligible: number;
  newAwards: number;
  version: number;
}

export interface TitleAdjustment {
  action: 'grant' | 'revoke';
  reason: string;
  key: string;
  /** 称号定义当前版本（不是用户获得记录的版本）。 */
  version: number;
}

export interface UserTitle {
  title: TitleDefinition;
  status: string;
  source?: string;
  earnedAt: string | null;
  expiresAt: string | null;
  earnedVersion: number;
  equipped: boolean;
  counts: number[];
  checkedAt: string | null;
  progressPending: boolean;
}

export interface TitleJobRow {
  id: string;
  version: number;
  cursor: string;
  maxUserId: string;
  processed: number;
  awarded: number;
  status: string;
  createdAt: string;
}

export interface TitleLogRow {
  id: string;
  userId: string;
  actorId: string;
  action: string;
  detail: unknown;
  createdAt: string;
}

export interface PointsConfig {
  version: number;
  rules: Record<string, GrowthRule>;
  acceptedExperience: GrowthRule;
}

export interface PollModerationItem {
  threadId: string;
  question: string;
  maxChoices: number;
  state: string;
  closesAt: string;
  createdAt: string;
}

export interface AdminBountyView extends BountyView {
  refundAttempts: number;
  refundErrorCode: string;
  refundFailedAt: string | null;
  refundNextAttemptAt: string | null;
}

export interface BountyRefundDiagnostics {
  active: number;
  due: number;
  failed: number;
  scheduled: number;
  oldestDueAt: string | null;
}

/** 后端固定的会员动作矩阵（顺序即展示顺序）。 */
export const MEMBER_ACTIONS = [
  'poll.create',
  'poll.vote',
  'bounty.create',
  'checkin.claim',
  'forum.read',
  'thread.create',
  'post.reply',
  'post.edit',
  'post.delete',
  'post.like',
  'thread.favorite',
  'post.report',
  'upload.image',
  'upload.file',
  'attachment.download',
  'post.link.direct',
  'post.skip.moderate',
];

/** 成长规则固定 5 类；积分规则固定 6 类。 */
export const MEMBERSHIP_RULES = ['active', 'thread', 'reply', 'like', 'digest'];
export const POINTS_RULES = ['active', 'thread', 'reply', 'like', 'digest', 'accepted'];

/** 称号条件支持的事件（来自后端 TitleMetrics）。 */
export const TITLE_METRICS: { value: string; label: string; forumScoped: boolean }[] = [
  { value: 'threads_created', label: '发帖数', forumScoped: true },
  { value: 'replies_created', label: '回复数', forumScoped: true },
  { value: 'likes_received', label: '获赞数', forumScoped: true },
  { value: 'featured_threads', label: '精华主题数', forumScoped: true },
  { value: 'accepted_replies', label: '被采纳回复数', forumScoped: true },
  { value: 'post_likes_max', label: '单帖最高赞', forumScoped: true },
  { value: 'experience', label: '经验值', forumScoped: false },
  { value: 'active_days', label: '活跃天数', forumScoped: false },
  { value: 'registered_days', label: '注册天数', forumScoped: false },
  { value: 'email_verified', label: '邮箱已验证', forumScoped: false },
];

export const LEVEL_BADGE_ICONS = ['', 'seedling', 'star', 'crown', 'shield', 'gem'];
// ---- 后台：报表 / 邮件队列 / 权限会话 / 诊断 ----

export interface AnalyticsSnapshotView {
  name: string;
  generatedAt: string;
  payload: unknown;
  periodStart: string;
  ageSeconds: number;
  stale: boolean;
  refreshIntervalSeconds: number;
  staleAfterSeconds: number;
}

export interface ForumStatsQueueStatus {
  asyncPublication: boolean;
  pending: number;
  retrying: number;
  oldestAgeSeconds: number;
}

export interface SearchIndexQueueStatus {
  pending: number;
  retrying: number;
  oldestAgeSeconds: number;
}

export interface DatabasePoolStats {
  max: number;
  total: number;
  acquired: number;
  idle: number;
  acquireCount: number;
  acquireDurationMs: number;
  emptyAcquireCount: number;
  canceledAcquireCount: number;
}

export interface DatabaseWorkloadStats {
  transactions: number;
  readIO: number;
  hitIO: number;
  cacheHitRatio: number;
}

export interface AdminDiagnosticsView {
  databasePool: DatabasePoolStats;
  databaseWorkload: DatabaseWorkloadStats;
  lockWaits: number;
  forumStats: ForumStatsQueueStatus;
  searchIndex: SearchIndexQueueStatus;
}

export interface EmailJobView {
  id: string;
  userId: string;
  kind: string;
  status: string;
  attempts: number;
  retries: number;
  version: number;
  nextAttemptAt: string;
  expiresAt: string;
  createdAt: string;
  sentAt: string | null;
  lastError: string;
}

export interface EmailQueueView {
  items: EmailJobView[];
  counts: Record<string, number>;
  nextBefore: string;
  smtpEnabled: boolean;
}

export interface AdminPermsView {
  points: string[];
  /** 角色 ID（0 会员 / 1 管理员 / 2 版主）→ 权限点 → 是否允许。 */
  matrix: Record<string, Record<string, boolean>>;
}

export interface SessionDeviceView {
  id: string;
  name: string;
  userAgent: string;
  maskedIp: string;
  createdAt: string;
  lastSeenAt: string;
  expiresAt: string;
  revokedAt: string | null;
  current: boolean;
  status: string;
}

export interface MemberDecisionView {
  allowed: boolean;
  reason: string;
  action: string;
  limit: number;
  used: number;
}

export interface MemberLogRow {
  id: string;
  userId: string;
  actorId: string;
  action: string;
  detail: unknown;
  createdAt: string;
}

/** 后台角色标签（与后端 perm.Role 对应）。 */
export const ADMIN_ROLES: { id: string; label: string }[] = [
  { id: '0', label: '会员' },
  { id: '2', label: '版主' },
  { id: '1', label: '管理员' },
];
