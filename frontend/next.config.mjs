/** @type {import('next').NextConfig} */
// SSR 内部上游地址只来自可信配置，不使用请求 Host 或用户参数拼接。
const API_INTERNAL_URL = process.env.API_INTERNAL_URL || 'http://127.0.0.1:8090';

const nextConfig = {
  reactStrictMode: true,
  poweredByHeader: false,
  // Arco Design React 同时发布 ESM/CJS，交给 Next 统一转译以确保 SSR 可用。
  transpilePackages: ['@arco-design/web-react'],
  async headers() {
    // 同一 URL 可能按 Accept 返回 HTML 或 Markdown：两种表示都必须声明 Vary: Accept，
    // 否则共享缓存可能把一种格式喂给另一种请求。
    return [
      // App Router 的页面响应会重写 Vary（只保留 RSC 相关值），所以这里只是补充声明；
      // 真正生效的是 middleware 里的 append 与 Markdown 路由处理器。HTML 表示本身是
      // private, no-store，不可被共享缓存存储，因此两种表示不会互相污染。
      { source: '/threads/:tid(\\d+)', headers: [{ key: 'Vary', value: 'Accept' }] },
    ];
  },
  async rewrites() {
    // 浏览器同域直达 Go：API、SSE 与受控媒体。
    return [
      { source: '/api/v1/:path*', destination: `${API_INTERNAL_URL}/api/v1/:path*` },
      { source: '/uploads/:path*', destination: `${API_INTERNAL_URL}/uploads/:path*` },
      { source: '/avatar/:path*', destination: `${API_INTERNAL_URL}/avatar/:path*` },
      { source: '/captcha/:path*', destination: `${API_INTERNAL_URL}/captcha/:path*` },
      { source: '/smiley/:path*', destination: `${API_INTERNAL_URL}/smiley/:path*` },
      { source: '/api/status', destination: `${API_INTERNAL_URL}/api/status` },
    ];
  },
};

export default nextConfig;
