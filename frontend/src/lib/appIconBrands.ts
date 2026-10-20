import androidStudioIcon from 'devicon/icons/androidstudio/androidstudio-original.svg';
import atomIcon from 'devicon/icons/atom/atom-original.svg';
import bashIcon from 'devicon/icons/bash/bash-original.svg';
import bitbucketIcon from 'devicon/icons/bitbucket/bitbucket-original.svg';
import chromeIcon from 'devicon/icons/chrome/chrome-original.svg';
import clionIcon from 'devicon/icons/clion/clion-original.svg';
import confluenceIcon from 'devicon/icons/confluence/confluence-original.svg';
import dockerIcon from 'devicon/icons/docker/docker-original.svg';
import electronIcon from 'devicon/icons/electron/electron-original.svg';
import figmaIcon from 'devicon/icons/figma/figma-original.svg';
import firefoxIcon from 'devicon/icons/firefox/firefox-original.svg';
import gitIcon from 'devicon/icons/git/git-original.svg';
import githubIcon from 'devicon/icons/github/github-original.svg';
import goIcon from 'devicon/icons/go/go-original.svg';
import golandIcon from 'devicon/icons/goland/goland-original.svg';
import intellijIcon from 'devicon/icons/intellij/intellij-original.svg';
import javaIcon from 'devicon/icons/java/java-original.svg';
import jiraIcon from 'devicon/icons/jira/jira-original.svg';
import neovimIcon from 'devicon/icons/neovim/neovim-original.svg';
import nodejsIcon from 'devicon/icons/nodejs/nodejs-original.svg';
import notionIcon from 'devicon/icons/notion/notion-original.svg';
import operaIcon from 'devicon/icons/opera/opera-original.svg';
import postmanIcon from 'devicon/icons/postman/postman-original.svg';
import pycharmIcon from 'devicon/icons/pycharm/pycharm-original.svg';
import pythonIcon from 'devicon/icons/python/python-original.svg';
import reactIcon from 'devicon/icons/react/react-original.svg';
import rustIcon from 'devicon/icons/rust/rust-original.svg';
import safariIcon from 'devicon/icons/safari/safari-original.svg';
import slackIcon from 'devicon/icons/slack/slack-original.svg';
import trelloIcon from 'devicon/icons/trello/trello-original.svg';
import vimIcon from 'devicon/icons/vim/vim-original.svg';
import vscodeIcon from 'devicon/icons/vscode/vscode-original.svg';
import webstormIcon from 'devicon/icons/webstorm/webstorm-original.svg';
import { cleanAppName, normalizeAppName } from './appIcons';

const BRAND_ICONS: Record<string, string> = {
  vscode: vscodeIcon,
  visualstudiocode: vscodeIcon,
  code: vscodeIcon,
  vscodium: vscodeIcon,
  googlechrome: chromeIcon,
  chrome: chromeIcon,
  chromium: chromeIcon,
  firefox: firefoxIcon,
  firefoxesr: firefoxIcon,
  safari: safariIcon,
  opera: operaIcon,
  slack: slackIcon,
  notion: notionIcon,
  goland: golandIcon,
  intellij: intellijIcon,
  intellijidea: intellijIcon,
  pycharm: pycharmIcon,
  webstorm: webstormIcon,
  clion: clionIcon,
  androidstudio: androidStudioIcon,
  postman: postmanIcon,
  figma: figmaIcon,
  trello: trelloIcon,
  jira: jiraIcon,
  confluence: confluenceIcon,
  bitbucket: bitbucketIcon,
  docker: dockerIcon,
  dockerdesktop: dockerIcon,
  git: gitIcon,
  github: githubIcon,
  githubdesktop: githubIcon,
  go: goIcon,
  golang: goIcon,
  python: pythonIcon,
  java: javaIcon,
  rust: rustIcon,
  nodejs: nodejsIcon,
  node: nodejsIcon,
  react: reactIcon,
  electron: electronIcon,
  vim: vimIcon,
  neovim: neovimIcon,
  atom: atomIcon,
  bash: bashIcon,
};

export function brandIconFor(name: string): string | null {
  const cleaned = cleanAppName(name);
  const direct = BRAND_ICONS[normalizeAppName(cleaned)];
  if (direct) return direct;

  const tokens = cleaned.toLowerCase().split(/[^\p{L}\p{N}]+/gu).filter(Boolean);
  for (const token of tokens) {
    const key = normalizeAppName(token);
    if (key.length >= 3 && BRAND_ICONS[key]) return BRAND_ICONS[key];
  }
  return null;
}
