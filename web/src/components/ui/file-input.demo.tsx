import { useState } from 'react';
import { FileInput } from './file-input';

export function FileInputDefaultDemo() {
  const [file, setFile] = useState<File | null>(null);
  return <FileInput file={file} onFileChange={setFile} label="选择一个文件" />;
}

export function FileInputAcceptDemo() {
  const [file, setFile] = useState<File | null>(null);
  return (
    <FileInput
      file={file}
      onFileChange={setFile}
      accept=".zip,application/zip"
      label="选择一个 zip 包"
    />
  );
}

export function FileInputErrorDemo() {
  return <FileInput onFileChange={() => {}} error="文件超过上限 200 MB" label="选择一个 zip 包" />;
}

export function FileInputDisabledDemo() {
  return <FileInput onFileChange={() => {}} disabled label="选择文件（禁用）" />;
}
