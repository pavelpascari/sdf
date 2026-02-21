import { defineCollection, z } from 'astro:content';

const docs = defineCollection({
  type: 'content',
  schema: z.object({
    title: z.string(),
    description: z.string().optional(),
    order: z.number().default(99),
  }),
});

const blog = defineCollection({
  type: 'content',
  schema: z.object({
    title: z.string(),
    description: z.string(),
    date: z.coerce.date(),
    tags: z.array(z.enum(['release', 'case-study', 'show-and-tell', 'tutorial'])),
    version: z.string().optional(),
    dateModified: z.coerce.date().optional(),
    author: z.string().default('SDF Team'),
    published: z.boolean().default(false),
  }),
});

export const collections = { docs, blog };
