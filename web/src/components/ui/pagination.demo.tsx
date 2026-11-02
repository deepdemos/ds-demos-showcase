import { useState } from 'react';
import { Pagination } from './pagination';

export function PaginationDefaultDemo() {
  const [page, setPage] = useState(2);
  return <Pagination page={page} pageCount={7} onPageChange={setPage} />;
}

export function PaginationDisabledDemo() {
  return <Pagination page={2} pageCount={7} onPageChange={() => {}} disabled />;
}
