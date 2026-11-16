import { Badge } from '../../components/ui/badge'
import { Button } from '../../components/ui/button'
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '../../components/ui/card'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../components/ui/tabs'

export type ModelProviderView = { provider: 'deepseek' | 'minimax'; configured: boolean; baseUrl: string; defaultModel: string }

type Props = { providers?: ModelProviderView[]; checking: boolean; message?: string; error?: string; onCheck: (provider: ModelProviderView['provider']) => void }

export function ModelServicesPage({ providers, checking, message, error, onCheck }: Props) {
  const configured = providers?.filter((provider) => provider.configured).length ?? 0
  return <section className="module-page" aria-labelledby="model-services-title">
    <div className="module-heading"><div><p className="eyebrow">MODEL GATEWAY</p><h2 id="model-services-title">模型服务</h2><p>管理首发模型供应商的连接状态。密钥仅保存在 API 服务端。</p></div><Badge variant={configured === 2 ? 'default' : 'warning'}>{configured}/2 可用</Badge></div>
    <Tabs defaultValue={providers?.[0]?.provider ?? 'deepseek'}><TabsList>{providers?.map((provider) => <TabsTrigger key={provider.provider} value={provider.provider}>{provider.provider === 'deepseek' ? 'DeepSeek' : 'MiniMax 国内站'}</TabsTrigger>)}</TabsList>{providers?.map((provider) => <TabsContent key={provider.provider} value={provider.provider}><Card className="model-service-card"><CardHeader><div className="model-service-title"><span className="provider-dot" data-configured={provider.configured} /><div><CardTitle>{provider.provider === 'deepseek' ? 'DeepSeek' : 'MiniMax 国内站'}</CardTitle><p>{provider.defaultModel}</p></div><Badge variant={provider.configured ? 'default' : 'warning'}>{provider.configured ? '已配置' : '未配置'}</Badge></div></CardHeader><CardContent><dl><div><dt>API 地址</dt><dd>{provider.baseUrl}</dd></div><div><dt>调用渠道</dt><dd>model_api</dd></div></dl></CardContent><CardFooter><Button variant="outline" onClick={() => onCheck(provider.provider)} disabled={!provider.configured || checking}>执行健康检查</Button></CardFooter></Card></TabsContent>)}</Tabs>
    {message && <p className="provider-message">{message} 连接正常</p>}{error && <p className="error" role="alert">{error}</p>}
  </section>
}

