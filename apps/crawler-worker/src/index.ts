import { createServer } from 'node:http'

const port = Number.parseInt(process.env.PORT ?? '8082', 10)
const server = createServer((request, response) => {
  if (request.url === '/healthz') {
    response.writeHead(200, { 'content-type': 'application/json' })
    response.end(JSON.stringify({ status: 'ok', service: 'crawler-worker' }))
    return
  }

  response.writeHead(404, { 'content-type': 'application/problem+json' })
  response.end(JSON.stringify({ title: 'Not Found', status: 404 }))
})

server.listen(port, '0.0.0.0', () => {
  process.stdout.write(JSON.stringify({ level: 'info', message: 'crawler worker started', port }) + '\n')
})

function shutdown(signal: string) {
  process.stdout.write(JSON.stringify({ level: 'info', message: 'shutting down', signal }) + '\n')
  server.close((error) => {
    process.exitCode = error ? 1 : 0
  })
}

process.on('SIGINT', () => shutdown('SIGINT'))
process.on('SIGTERM', () => shutdown('SIGTERM'))
