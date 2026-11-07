import { useState } from 'react';

import { Rating } from './rating';

export function RatingDefaultDemo() {
  const [score, setScore] = useState(3);
  return <Rating label="给这个作品打分" value={score} onChange={setScore} showValueText />;
}

export function RatingEmptyDemo() {
  const [score, setScore] = useState(0);
  return <Rating label="还没有打过分" value={score} onChange={setScore} showValueText />;
}

export function RatingDisabledDemo() {
  return (
    <Rating label="提交中不可操作" value={4} onChange={() => undefined} disabled showValueText />
  );
}
