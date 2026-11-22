import { AdminAppealsPage } from '@/routes/AdminAppealsPage';
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
import { HistoryPage } from '@/routes/HistoryPage';
import { HomePage } from '@/routes/HomePage';
import { LoginPage } from '@/routes/LoginPage';
import { ModelPage } from '@/routes/ModelPage';
import { MyDemosPage } from '@/routes/MyDemosPage';
import { NotFoundPage } from '@/routes/NotFoundPage';
import { NotificationsPage } from '@/routes/NotificationsPage';
import { PasswordResetPage } from '@/routes/PasswordResetPage';
import { ProposalDetailPage } from '@/routes/ProposalDetailPage';
import { RankingsPage } from '@/routes/RankingsPage';
import { RegisterPage } from '@/routes/RegisterPage';
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
  'demo-detail': <DemoDetailPage />,
  'demo-edit': <EditDemoPage />,
  'model-detail': <ModelPage />,
  rankings: <RankingsPage />,
  author: <AuthorPage />,
  classification: <ClassificationPage />,
  guidelines: <CommunityGuidelinesPage />,
  submit: <SubmitPage />,
  collections: <CollectionsPage />,
  'collection-detail': <CollectionDetailPage />,
  notifications: <NotificationsPage />,
  'my-demos': <MyDemosPage />,
  history: <HistoryPage />,
  login: <LoginPage />,
  register: <RegisterPage />,
  'verify-email': <VerifyEmailPage />,
  'password-reset': <PasswordResetPage />,
  'admin-inbox': <AdminInboxPage />,
  'admin-reviews': <AdminReviewPage />,
  'admin-proposal': <ProposalDetailPage />,
  'admin-appeals': <AdminAppealsPage />,
  'admin-models': <AdminModelsPage />,
  'not-found': <NotFoundPage />,
} satisfies RouteElementMap;
