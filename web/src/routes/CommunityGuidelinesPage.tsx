import { Link } from 'react-router';

/** 社区规范（#463）：静态页，给 removed 墓碑与页脚一个稳定落点。 */
export function CommunityGuidelinesPage() {
  return (
    <section className="mx-auto w-full max-w-3xl px-4 py-8">
      <h1 className="text-2xl font-semibold text-gray-900 dark:text-gray-100">社区规范</h1>
      <p className="mt-2 text-sm text-gray-500 dark:text-gray-400">
        本站展示 AI 生成与 AI 辅助创作的作品。发布或参与之前，请先确认下面几条。
      </p>

      <h2 className="mt-6 text-lg font-semibold">不允许的内容</h2>
      <ul className="mt-2 list-disc space-y-1 pl-5 text-sm">
        <li>违反法律法规的内容，以及协助规避监管的用法。</li>
        <li>侵犯他人著作权、商标权、肖像权或隐私的内容。</li>
        <li>未经同意公开他人的个人信息、私密对话或可识别身份的材料。</li>
        <li>色情、暴力、仇恨、骚扰，以及以伤害特定人群为目的的内容。</li>
        <li>恶意代码、钓鱼页面、诱导下载或其他危害访问者设备的作品。</li>
      </ul>

      <h2 className="mt-6 text-lg font-semibold">发布者需要做到</h2>
      <ul className="mt-2 list-disc space-y-1 pl-5 text-sm">
        <li>作品必须能在站内预览；需要外部资源的，说明依赖与运行方式。</li>
        <li>如实填写使用的模型与工具；模型声明是作者自述，不是平台背书。</li>
        <li>转载、二创或使用他人素材时，确认授权并在简介里说明来源。</li>
        <li>收到审核意见后按说明修改；被驳回不等于账号被处罚。</li>
      </ul>

      <h2 className="mt-6 text-lg font-semibold">处置与申诉</h2>
      <p className="mt-2 text-sm">
        违反规范的作品会被下架：作者可以自主下架，平台也可以因违规移除。被移除的作品页
        仍保留标题与处理时间，作者可以在墓碑页提交一次申诉；申诉处理期间作品不会恢复公开，
        也不能修改。申诉结果会显示在同一个位置。
      </p>
      <p className="mt-2 text-sm">
        对规范本身有疑问，可以在站内反馈渠道提出；不要用重复提交申诉来代替沟通。
      </p>

      <p className="mt-6 text-sm">
        <Link className="underline" to="/demos">
          返回作品库
        </Link>
      </p>
    </section>
  );
}
