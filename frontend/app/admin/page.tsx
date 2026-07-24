'use client';

import { Card } from '@/components/ui/Card';

const API_URL = process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:8080';

export default function AdminPage() {
  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-2xl font-semibold text-text">Admin</h1>
      <Card>
        <div className="flex flex-col gap-1">
          <span className="text-xs uppercase text-text-muted">API base URL</span>
          <span className="font-mono text-text">{API_URL}</span>
        </div>
      </Card>
    </div>
  );
}
