export type ModelProviderView = { provider: 'deepseek' | 'minimax'; configured: boolean; baseUrl: string; defaultModel: string }

type Props = { providers?: ModelProviderView[]; checking: boolean; message?: string; error?: string; onCheck: (provider: ModelProviderView['provider']) => void }

export function ModelServicesPage({ providers, checking, message, error, onCheck }: Props) {
  const configured = providers?.filter((provider) => provider.configured).length ?? 0
  return <section className="module-page" aria-labelledby="model-services-title">
    <div className="module-heading"><div><p className="eyebrow">MODEL GATEWAY</p><h2 id="model-services-title">模型服务</h2><p>管理首发模型供应商的连接状态。密钥仅保存在 API 服务端。</p></div><span>{configured}/2 可用</span></div>
    <div className="model-service-grid">{providers?.map((provider) => <article key={provider.provider}><div className="model-service-title"><span className="provider-dot" data-configured={provider.configured} /><div><h3>{provider.provider === 'deepseek' ? 'DeepSeek' : 'MiniMax 国内站'}</h3><p>{provider.defaultModel}</p></div><em data-configured={provider.configured}>{provider.configured ? '已配置' : '未配置'}</em></div><dl><div><dt>API 地址</dt><dd>{provider.baseUrl}</dd></div><div><dt>调用渠道</dt><dd>model_api</dd></div></dl><button onClick={() => onCheck(provider.provider)} disabled={!provider.configured || checking}>执行健康检查</button></article>)}</div>
    {message && <p className="provider-message">{message} 连接正常</p>}{error && <p className="error" role="alert">{error}</p>}
  </section>
}
