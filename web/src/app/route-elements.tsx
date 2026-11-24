import { AdminAppealsPage } from '@/routes/AdminAppealsPage';
import { BenchmarkDetailPage } from '@/routes/BenchmarkDetailPage';
import { BenchmarkLeaderboardsPage } from '@/routes/BenchmarkLeaderboardsPage';
import { BenchmarkOverviewPage } from '@/routes/BenchmarkOverviewPage';
import { BenchmarksPage } from '@/routes/BenchmarksPage';
import { BenchmarkSourcesPage } from '@/routes/BenchmarkSourcesPage';
import { AdminInboxPage } from '@/routes/AdminInboxPage';
import { AdminModelsPage } from '@/routes/AdminModelsPage';
import { AdminReviewPage } from '@/routes/AdminReviewPage';
import { AuthorPage } from '@/routes/AuthorPage';
import { ClassificationPage } from '@/routes/ClassificationPage';
import { CollectionDetailPage } from '@/routes/CollectionDetailPage';
import { CollectionsPage } from '@/routes/CollectionsPage';
import { CommunityGuidelinesPage } from '@/routes/CommunityGuidelinesPage';
import { DemoDetailPage } from '@/routes/DemoDetailPage';
import { DemosPage } from '@/routes/DemosPage';
import { EditDemoPage } from '@/routes/EditDemoPage';
import { ExplorePage } from '@/routes/ExplorePage';
import { HistoryPage } from '@/routes/HistoryPage';
import { HomePage } from '@/routes/HomePage';
import { LoginPage } from '@/routes/LoginPage';
import { ModelPage } from '@/routes/ModelPage';
import { ModelsPage } from '@/routes/ModelsPage';
import { MyDemosPage } from '@/routes/MyDemosPage';
import { NotFoundPage } from '@/routes/NotFoundPage';
import { NotificationsPage } from '@/routes/NotificationsPage';
import { PasswordResetPage } from '@/routes/PasswordResetPage';
import { ProposalDetailPage } from '@/routes/ProposalDetailPage';
import { ProposalsPage } from '@/routes/ProposalsPage';
import { RankingsPage } from '@/routes/RankingsPage';
import { RegisterPage } from '@/routes/RegisterPage';
import { SearchPage } from '@/routes/SearchPage';
import { SettingsPage } from '@/routes/SettingsPage';
import { AboutPage } from '@/routes/AboutPage';
import { SubmitPage } from '@/routes/SubmitPage';
import { VerifyEmailPage } from '@/routes/VerifyEmailPage';
import type { RouteElementMap } from './routes';

/**
 * 路由 id → 页面元素。
 *
 * 为什么单独一张表：routes.ts 只放纯数据，测试与导航不必 import 页面；
 * 这里集中 import，新增页面只改一处。
 */
export const routeElements: RouteElementMap = {
  home: <HomePage />,
  demos: <DemosPage />,
  explore: <ExplorePage />,
  'demo-detail': <DemoDetailPage />,
  'demo-edit': <EditDemoPage />,
  models: <ModelsPage />,
  'model-detail': <ModelPage />,
  benchmarks: <BenchmarksPage />,
  'benchmark-sources': <BenchmarkSourcesPage />,
  'benchmark-overview': <BenchmarkOverviewPage />,
  'benchmark-leaderboards': <BenchmarkLeaderboardsPage />,
  'benchmark-detail': <BenchmarkDetailPage />,
  rankings: <RankingsPage />,
  author: <AuthorPage />,
  classification: <ClassificationPage />,
  guidelines: <CommunityGuidelinesPage />,
  about: <AboutPage />,
  search: <SearchPage />,
  submit: <SubmitPage />,
  collections: <CollectionsPage />,
  'collection-detail': <CollectionDetailPage />,
  notifications: <NotificationsPage />,
  'my-demos': <MyDemosPage />,
  history: <HistoryPage />,
  settings: <SettingsPage />,
  login: <LoginPage />,
  register: <RegisterPage />,
  'verify-email': <VerifyEmailPage />,
  'password-reset': <PasswordResetPage />,
  'admin-inbox': <AdminInboxPage />,
  'admin-reviews': <AdminReviewPage />,
  'admin-proposals': <ProposalsPage />,
  'admin-proposal': <ProposalDetailPage />,
  'admin-appeals': <AdminAppealsPage />,
  'admin-models': <AdminModelsPage />,
  'not-found': <NotFoundPage />,
} satisfies RouteElementMap;
