import type { MetadataRoute } from 'next';
import { SITE_URL } from '@/lib/site-url';

/**
 * robots 与页面的 noindex 策略保持一致，但不作为访问控制：
 * 真正的权限仍由 API 与页面守卫执行。
 */
export default function robots(): MetadataRoute.Robots {
  return {
    rules: [
      {
        userAgent: '*',
        allow: '/',
        disallow: [
          '/admin/',
          '/api/',
          '/me/',
          '/settings/',
          '/new',
          '/search',
          '/login',
          '/register',
          '/password/',
          '/verify',
          '/reset',
          '/setup',
        ],
      },
    ],
    sitemap: SITE_URL + '/sitemap.xml',
    host: SITE_URL,
  };
}
